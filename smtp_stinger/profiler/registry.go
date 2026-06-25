package profiler

type Profile interface {
	Name() string

	Start() error
	Stop() error
}

type Factory func(Config) (Profile, error)

var registry = map[string]Factory{}

// Register adds a profile implementation to the global registry.
func Register(name string, factory Factory) {
	if _, exists := registry[name]; exists {
		panic("profiler: profile already registered: " + name)
	}

	registry[name] = factory
}

func Registered() []string {
	names := make([]string, 0, len(registry))

	for name := range registry {
		names = append(names, name)
	}

	return names
}
