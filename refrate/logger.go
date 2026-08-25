package refrate

import "log/slog"

// pkgLogger is the package-level logger for upstream call logging.
// Set via SetLogger; nil means slog.Default().
var pkgLogger *slog.Logger

// SetLogger configures the package-level logger for upstream call logging
// used by both CurrencyAPI and ExchangeRateAPI providers.
func SetLogger(l *slog.Logger) { pkgLogger = l }

func log() *slog.Logger {
	if pkgLogger != nil {
		return pkgLogger
	}
	return slog.Default()
}
