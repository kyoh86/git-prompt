package git

func intSetter(r int, err error) func(v *int) error {
	if err != nil {
		return func(*int) error { return err }
	}
	return func(v *int) error {
		*v = r
		return nil
	}
}

func boolSetter(r bool, err error) func(v *bool) error {
	if err != nil {
		return func(*bool) error { return err }
	}
	return func(v *bool) error {
		*v = r
		return nil
	}
}

func stringSetter(r string, err error) func(v *string) error {
	if err != nil {
		return func(*string) error { return err }
	}
	return func(v *string) error {
		*v = r
		return nil
	}
}
