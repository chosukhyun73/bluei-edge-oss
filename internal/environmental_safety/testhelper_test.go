package environmental_safety

import (
	"io"
	"log/slog"
)

func silentLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
