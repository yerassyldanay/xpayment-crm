package domain

import (
	"fmt"
	"strings"
)

// ValidateSnapshot gates a publish (docs/03 · validate on load, fail loudly):
// every price/limit/placeholder token used in any topic must resolve, and every
// asset url must be non-empty. A topic present in only one language is a
// non-blocking warning. Build the snapshot, validate, and only then swap the
// live pointer.
func ValidateSnapshot(snap *Snapshot) (warnings []string, err error) {
	for _, t := range snap.Topics {
		for _, m := range tokenRE.FindAllStringSubmatch(t.BodyMD, -1) {
			ns, key := m[1], m[2]
			switch ns {
			case "price", "limit":
				if _, ok := snap.Prices.Tariffs[key]; !ok {
					return warnings, fmt.Errorf("topic %q (%s): unknown token {{%s.%s}}", t.Slug, t.Language, ns, key)
				}
			default:
				if _, ok := snap.Prices.Placeholders[ns+"."+key]; !ok {
					return warnings, fmt.Errorf("topic %q (%s): unknown placeholder {{%s.%s}}", t.Slug, t.Language, ns, key)
				}
			}
		}
	}

	langs := map[string]map[string]bool{}
	for _, t := range snap.Topics {
		if langs[t.Slug] == nil {
			langs[t.Slug] = map[string]bool{}
		}
		langs[t.Slug][t.Language] = true
	}
	for slug, l := range langs {
		if !l["ru"] || !l["kk"] {
			warnings = append(warnings, fmt.Sprintf("topic %q exists in only one language", slug))
		}
	}

	for _, a := range snap.Assets {
		if strings.TrimSpace(a.URL) == "" {
			return warnings, fmt.Errorf("asset %q has empty url", a.Ref)
		}
	}
	return warnings, nil
}
