package resolver

// ResolveLibraries resolves distinct shared-library SONAMEs. Resolvers with
// local package databases can implement ResolveMany to avoid repeated scans.
// The existing single Resolve contract is retained for all other callers.
func ResolveLibraries(r DependencyResolver, libs []string) map[string]string {
	if len(libs) == 0 || r == nil {
		return nil
	}
	if batch, ok := r.(interface {
		ResolveMany([]string) map[string]string
	}); ok {
		return batch.ResolveMany(libs)
	}
	found := make(map[string]string, len(libs))
	for _, name := range libs {
		if name == "" {
			continue
		}
		if _, ok := found[name]; ok {
			continue
		}
		if pkg, err := r.Resolve(name); err == nil && pkg != "" {
			found[name] = pkg
		}
	}
	return found
}
