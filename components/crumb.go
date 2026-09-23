package components

// CrumbSegment resolves a breadcrumb segment: the short form when requested and set, else
// crumb, else fallback.
func CrumbSegment(short bool, crumbShort, crumb, fallback string) string {
	if short && crumbShort != "" {
		return crumbShort
	}
	if crumb != "" {
		return crumb
	}
	return fallback
}
