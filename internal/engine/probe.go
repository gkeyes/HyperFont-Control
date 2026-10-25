package engine

import "os"

// Probe reads only stock system files. It never initializes the data store,
// starts a service, mounts anything or changes the installed module.
func Probe() (any, error) {
	results := map[string]any{}
	for _, path := range []string{"/product/fonts/MiSansVF.ttf", "/product/fonts/MiSansLatinVF.ttf", "/system/fonts/Roboto-Regular.ttf"} {
		b, e := os.ReadFile(path)
		if e != nil {
			results[path] = map[string]string{"error": e.Error()}
			continue
		}
		faces, e := ParseFont(b)
		if e != nil {
			return nil, e
		}
		results[path] = map[string]any{"faces": faces, "sha256": digest(b)}
	}
	return map[string]any{"version": Version, "sdk": property("ro.build.version.sdk"), "fonts": results, "writes": false, "mounts": false}, nil
}
