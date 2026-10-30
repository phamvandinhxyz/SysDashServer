package collector

import "fmt"

// FormatBinary formats bytes using 1024-based units (KiB, MiB, GiB...).
// This matches what `htop`, `free -h`, `df -h` and `docker stats` display on Linux.
func FormatBinary(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

// FormatDecimal formats bytes using 1000-based units (kB, MB, GB...).
// This matches what `docker images` / `docker system df` display (Docker CLI uses SI units for image sizes).
func FormatDecimal(b uint64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "kMGTPE"[exp])
}

// FormatBytes is kept for backward compatibility (binary units).
func FormatBytes(b uint64) string {
	return FormatBinary(b)
}
