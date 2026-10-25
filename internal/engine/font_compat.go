package engine

const fontCompatMarker = "HFC-logical-weights"
const fontCompatSignature = "hyperfont-logical-weights-v1\n"

// The process adapter is needed only when the Xiaomi default text roles have
// custom fonts. Restoring system fonts also removes the transient marker.
func needsFontCompat(g Generation) bool {
	custom := g.Profile.Slots["chinese"].Mode != "system" || g.Profile.Slots["latinSans"].Mode != "system"
	if !custom || g.Profile.IsSystem() {
		return false
	}
	for _, t := range g.Targets {
		if t.Path == "/product/etc/misans_variable_fonts.xml" || t.Path == "/product/etc/mi_fonts_customization.xml" {
			return true
		}
	}
	return false
}
