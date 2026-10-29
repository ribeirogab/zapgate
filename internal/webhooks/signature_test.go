package webhooks_test

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/ribeirogab/zapgate/internal/webhooks"
)

func TestSignatureMatchesTheStandardWebhooksTestVector(t *testing.T) {
	signature, err := webhooks.Sign(
		"msg_p5jXN8AQM9LWM0D4loKWxJek",
		time.Unix(1614265330, 0),
		[]byte(`{"test": 2432232314}`),
		"whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
	)

	if err != nil {
		t.Fatal(err)
	}
	if signature != "v1,g0hM9SsE+OTPJTGt/tmIKtSyZlE3uFJELVlNIOLJ1OE=" {
		t.Fatalf("signature %q", signature)
	}
}

func TestSignatureRefusesASecretThatIsNotBase64(t *testing.T) {
	if _, err := webhooks.Sign("msg_1", time.Unix(1614265330, 0), []byte(`{}`), "whsec_not base64"); err == nil {
		t.Fatal("a secret that is not base64 signed the body")
	}
}

func TestNewSecretIsThePrefixAndThirtyTwoRandomBytes(t *testing.T) {
	first, err := webhooks.NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	second, err := webhooks.NewSecret()
	if err != nil {
		t.Fatal(err)
	}

	encoded, ok := strings.CutPrefix(first, "whsec_")
	if !ok {
		t.Fatalf("secret %q has no whsec_ prefix", first)
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != 32 {
		t.Fatalf("secret %q is not the base64 of 32 bytes: %d bytes, %v", first, len(key), err)
	}
	if first == second {
		t.Fatal("two secrets are equal")
	}
}
