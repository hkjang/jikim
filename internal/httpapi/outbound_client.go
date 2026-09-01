package httpapi

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"
)

const internalCAPath = "/app/certs/internal-ca.crt"

func newOutboundHTTPClient(logger *slog.Logger, timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if certificate, readErr := os.ReadFile(internalCAPath); readErr == nil {
		if len(certificate) > 1<<20 || !pool.AppendCertsFromPEM(certificate) {
			logger.Warn("내부 CA 파일을 읽었지만 유효한 PEM 인증서가 없습니다", "path", internalCAPath)
		} else {
			logger.Info("내부 CA 인증서를 outbound TLS에 추가했습니다", "path", internalCAPath)
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		logger.Warn("내부 CA 파일을 읽지 못했습니다", "path", internalCAPath, "error", readErr)
	}
	transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	return &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}
