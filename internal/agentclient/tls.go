package agentclient

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
)

func clientTLSConfig(cfg Config) (*tls.Config, error) {
	out := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: !cfg.Security.VerifyServerTLS}
	if cfg.Security.CAFile != "" {
		pem, err := os.ReadFile(cfg.Security.CAFile)
		if err != nil {
			return nil, err
		}
		pool, err := x509.SystemCertPool()
		if err != nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("server CA contains no certificates")
		}
		out.RootCAs = pool
	}
	if cfg.Security.CertFile != "" || cfg.Security.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.Security.CertFile, cfg.Security.KeyFile)
		if err != nil {
			return nil, err
		}
		out.Certificates = []tls.Certificate{cert}
	}
	return out, nil
}
