package tunnel

import (
	"io"
	"log/slog"
)

var logger *slog.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))

// SetLogger allows library users to inject their own slog.Logger.
func SetLogger(l *slog.Logger) {
	if l != nil {
		logger = l
	} else {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
}
