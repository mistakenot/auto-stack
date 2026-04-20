package port

import "fmt"

func Allocate(names []string, base, stride, slot int) (map[string]int, error) {
	if len(names) > stride {
		return nil, fmt.Errorf("too many port names (%d) for configured stride (%d)", len(names), stride)
	}

	ports := make(map[string]int, len(names))
	for i, name := range names {
		ports[name] = base + slot*stride + i
	}
	return ports, nil
}
