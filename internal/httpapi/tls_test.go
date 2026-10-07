package httpapi

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"github.com/aimdotsh/dbops/internal/agentgateway"
	"github.com/gorilla/websocket"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTLSConfigurationFailsClosed(t *testing.T) {
	s := &Server{http: &http.Server{}}
	if err := s.ConfigureTLS("", "", "ca.pem"); err == nil {
		t.Fatal("accepted client CA without HTTPS")
	}
	if err := s.ConfigureTLS("missing-cert", "missing-key", ""); err == nil {
		t.Fatal("accepted missing credentials")
	}
	if err := s.ConfigureTLS("", "", ""); err != nil {
		t.Fatal(err)
	}
}
func TestAgentMTLSRequiresTrustedMatchingIdentity(t *testing.T) {
	dir := t.TempDir()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca, err = x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	caPath := filepath.Join(dir, "ca.pem")
	if err = os.WriteFile(caPath, caPEM, 0600); err != nil {
		t.Fatal(err)
	}
	issue := func(name string, serial int64, usage x509.ExtKeyUsage) (tls.Certificate, string, string) {
		t.Helper()
		leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		cert := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: name}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}}
		raw, err := x509.CreateCertificate(rand.Reader, cert, ca, &leafKey.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw})
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(leafKey)})
		certPath, keyPath := filepath.Join(dir, name+".pem"), filepath.Join(dir, name+".key")
		if err = os.WriteFile(certPath, certPEM, 0600); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(keyPath, keyPEM, 0600); err != nil {
			t.Fatal(err)
		}
		pair, err := tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			t.Fatal(err)
		}
		return pair, certPath, keyPath
	}
	_, certPath, keyPath := issue("server", 2, x509.ExtKeyUsageServerAuth)
	clientCert, _, _ := issue("agent-one", 3, x509.ExtKeyUsageClientAuth)
	gateway := agentgateway.New(nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), 30, "test", false)
	gateway.RequireMTLS(true)
	server := &Server{http: &http.Server{}}
	if err = server.ConfigureTLS(certPath, keyPath, caPath); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewUnstartedServer(gateway)
	ts.TLS = server.http.TLSConfig
	ts.StartTLS()
	defer ts.Close()
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(caPEM)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}}
	resp, err := client.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("anonymous agent accepted: %d", resp.StatusCode)
	}
	dialer := websocket.Dialer{TLSClientConfig: &tls.Config{RootCAs: roots, Certificates: []tls.Certificate{clientCert}, MinVersion: tls.VersionTLS12}}
	conn, _, err := dialer.Dial(strings.Replace(ts.URL, "https://", "wss://", 1), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err = conn.WriteJSON(map[string]any{"type": "hello", "data": map[string]any{"agent_uuid": "different-agent"}}); err != nil {
		t.Fatal(err)
	}
	_, message, err := conn.ReadMessage()
	if err != nil {
		if closeErr, ok := err.(*websocket.CloseError); ok && closeErr.Code == websocket.ClosePolicyViolation && strings.Contains(closeErr.Text, "certificate identity mismatch") {
			return
		}
		t.Fatal(err)
	}
	if !strings.Contains(string(message), "certificate identity mismatch") {
		t.Fatalf("identity not rejected: %s", message)
	}
}
