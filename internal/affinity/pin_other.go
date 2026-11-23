//go:build !linux

package affinity

func platformListTasks() ([]int, error) {
	return nil, ErrUnsupported
}

func platformGetTask(int) (Set, error) {
	return Set{}, ErrUnsupported
}

func platformSetTask(int, Set) error {
	return ErrUnsupported
}
