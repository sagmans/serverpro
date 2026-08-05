package cli

func (a *app) promptDefaultWhen(ask bool, label string, value *string) error {
	if *value != "" && !ask {
		return nil
	}
	selected, err := a.promptDefault(label, *value)
	if err != nil {
		return err
	}
	*value = selected
	return nil
}

func (a *app) promptChoiceWhen(ask bool, label string, value *string, choices []choice) error {
	if *value != "" && !ask {
		return nil
	}
	selected, err := a.promptChoice(label, *value, choices)
	if err != nil {
		return err
	}
	*value = selected
	return nil
}
