package security

import "testing"

func TestCipherRoundTrip(t *testing.T) {
	c, err := NewCipher("0123456789abcdef-test-master-key")
	if err != nil {
		t.Fatal(err)
	}
	enc, err := c.EncryptString("top-secret")
	if err != nil {
		t.Fatal(err)
	}
	if enc == "top-secret" {
		t.Fatal("ciphertext equals plaintext")
	}
	got, err := c.DecryptString(enc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "top-secret" {
		t.Fatalf("unexpected plaintext: %q", got)
	}
}
