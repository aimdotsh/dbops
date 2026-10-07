package httpapi

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
)

// ConfigureTLS verifies optional client certificates at transport level. The
// Agent endpoint separately requires a verified certificate when mTLS is enabled.
func (s *Server) ConfigureTLS(certFile, keyFile, clientCA string) error {
	if certFile == "" && keyFile == "" {
		if clientCA != "" {
			return errors.New("client CA requires server TLS")
		}
		return nil
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return err
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}
	if clientCA != "" {
		pem, err := os.ReadFile(clientCA)
		if err != nil {
			return err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return errors.New("client CA contains no certificates")
		}
		cfg.ClientCAs = pool
		cfg.ClientAuth = tls.VerifyClientCertIfGiven
	}
	s.http.TLSConfig = cfg
	return nil
}
