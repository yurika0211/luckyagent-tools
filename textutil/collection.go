package textutil

// DedupStringsLimit removes duplicates while preserving the original order.
// A non-positive limit keeps all distinct strings.
func DedupStringsLimit(items []string, limit int) []string {
	if limit <= 0 {
		limit = len(items)
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, min(len(items), limit))
	for _, item := range items {
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
		if len(out) >= limit {
			break
		}
	}
	return out
}
