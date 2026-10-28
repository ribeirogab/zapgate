package cli

import (
	"fmt"
	"strconv"

	"github.com/ribeirogab/zapgate/internal/admin"
)

var (
	statusHeader = []string{"NAME", "PHONE", "STATE", "MESSAGES", "MEDIA", "RETENTION", "DESCRIPTION"}
	byteUnits    = []string{"kB", "MB", "GB", "TB"}
)

func statusRows(accounts []admin.AccountStatus) [][]string {
	rows := make([][]string, 0, len(accounts))
	for _, a := range accounts {
		rows = append(rows, []string{
			a.Name,
			a.Phone,
			a.State,
			strconv.FormatInt(a.Messages, 10),
			humanBytes(a.MediaBytes),
			retentionLabel(a.AccountView),
			a.Description,
		})
	}
	return rows
}

func humanBytes(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	unit := ""
	for _, next := range byteUnits {
		value /= 1000
		unit = next
		if value < 1000 {
			break
		}
	}
	return fmt.Sprintf("%.1f %s", value, unit)
}

func retentionLabel(a admin.AccountView) string {
	label := monthsLabel(a.RetentionMonths)
	if a.RetentionDefault {
		return label + " (default)"
	}
	return label
}

func monthsLabel(months int) string {
	if months == 1 {
		return "1 month"
	}
	return fmt.Sprintf("%d months", months)
}

func retentionLine(pass admin.RetentionView) string {
	switch {
	case pass.StartedAt == "":
		return "retention: no pass yet"
	case pass.Running:
		return fmt.Sprintf("retention: a pass runs since %s; it deleted %s and %s so far", pass.StartedAt, count(pass.DeletedMessages, "message"), count(pass.DeletedFiles, "media file"))
	}
	return fmt.Sprintf("retention: the last pass ran at %s and deleted %s and %s; the next pass runs a day after it", pass.StartedAt, count(pass.DeletedMessages, "message"), count(pass.DeletedFiles, "media file"))
}

func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
