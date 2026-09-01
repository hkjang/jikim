package httpapi

import "testing"

func TestWebhookSignatureUsesTimestampAndBody(t *testing.T) {
	first := webhookSignature("01234567890123456789012345678901", "1700000000", []byte(`{"event":"secret.rotated"}`))
	if first != "sha256=fe1edb4066e10ba1db327adf23217271f1a43c6566ba03e99b22aa5cef8bb44d" {
		t.Fatalf("unexpected signature: %s", first)
	}
	if second := webhookSignature("01234567890123456789012345678901", "1700000001", []byte(`{"event":"secret.rotated"}`)); second == first {
		t.Fatal("timestamp was not covered by the signature")
	}
}
