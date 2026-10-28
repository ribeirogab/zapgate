package live

import (
	"context"
	"errors"
	"fmt"

	"go.mau.fi/whatsmeow"

	"github.com/ribeirogab/zapgate/internal/whatsapp"
)

func (c *client) DownloadMedia(ctx context.Context, media whatsapp.MediaDownload, file whatsmeow.File) error {
	if !c.cli.IsConnected() {
		return whatsapp.ErrNotConnected
	}
	err := c.cli.DownloadMediaWithPathToFile(ctx, media.DirectPath, media.FileEncSHA256, media.FileSHA256, media.MediaKey, media.Type, "", false, file)
	if goneFromTheCDN(err) {
		return fmt.Errorf("%w: %v", whatsapp.ErrMediaGone, err)
	}
	if errors.Is(err, whatsmeow.ErrNotConnected) {
		return fmt.Errorf("%w: %v", whatsapp.ErrNotConnected, err)
	}
	return err
}

func goneFromTheCDN(err error) bool {
	for _, gone := range []error{whatsmeow.ErrMediaDownloadFailedWith403, whatsmeow.ErrMediaDownloadFailedWith404, whatsmeow.ErrMediaDownloadFailedWith410, whatsmeow.ErrNoURLPresent} {
		if errors.Is(err, gone) {
			return true
		}
	}
	return false
}
