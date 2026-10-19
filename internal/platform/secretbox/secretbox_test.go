package secretbox

import (
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func testKey(t *testing.T) string {
	t.Helper()

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return base64.StdEncoding.EncodeToString(key)
}

func newBox(t *testing.T) *Box {
	t.Helper()

	box, err := New(testKey(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return box
}

func TestSealOpenRoundTrip(t *testing.T) {
	box := newBox(t)
	const secret = "sk-very-secret-value"

	sealed, err := box.Seal(secret)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if sealed == secret {
		t.Fatal("Seal returned the plaintext unchanged")
	}

	opened, err := box.Open(sealed)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if opened != secret {
		t.Fatalf("Open returned %q, want %q", opened, secret)
	}
}

// 同一明文两次加密必须得到不同密文：nonce 每次重新生成，这既是 GCM 的要求，
// 也顺带保证不能靠比对密文判断两个 provider 是否用了同一个 key。
func TestSealIsNonDeterministic(t *testing.T) {
	box := newBox(t)

	first, err := box.Seal("same")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	second, err := box.Seal("same")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if first == second {
		t.Fatal("two Seals of the same plaintext produced identical ciphertext")
	}
}

func TestOpenRejectsTamperedCiphertext(t *testing.T) {
	box := newBox(t)

	sealed, err := box.Seal("secret")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	raw[len(raw)-1] ^= 0xff

	if _, err := box.Open(base64.StdEncoding.EncodeToString(raw)); err == nil {
		t.Fatal("Open accepted a tampered ciphertext")
	}
}

func TestOpenWithDifferentKeyFails(t *testing.T) {
	sealed, err := newBox(t).Seal("secret")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	if _, err := newBox(t).Open(sealed); err == nil {
		t.Fatal("Open with a different key succeeded")
	}
}

func TestNewRejectsInvalidKey(t *testing.T) {
	cases := map[string]string{
		"empty":        "",
		"not base64":   "not-base64!!",
		"wrong length": base64.StdEncoding.EncodeToString([]byte("too-short")),
		"16-byte AES":  base64.StdEncoding.EncodeToString(make([]byte, 16)),
	}

	for name, key := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := New(key); err == nil {
				t.Fatal("New accepted an invalid key")
			}
		})
	}
}
