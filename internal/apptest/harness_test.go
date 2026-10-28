package apptest_test

import (
	"testing"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

func TestRestartOpensANewWhatsAppHandleAndClosesTheOld(t *testing.T) {
	h := apptest.Start(t)

	h.Restart(t)

	if got := h.WhatsApp.Opens(); got != 2 {
		t.Fatalf("opens %d, want 2", got)
	}
	if got := h.WhatsApp.OpenHandles(); got != 1 {
		t.Fatalf("open handles %d, want 1", got)
	}
}
