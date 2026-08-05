package config

func (c Config) Validate() error {
	for _, validate := range []func() error{
		c.validateRequired,
		c.validateResourceNames,
		c.validateNamespaceScopes,
		c.validateMVPConstraints,
		c.validateAdminUsername,
	} {
		if err := validate(); err != nil {
			return err
		}
	}
	return nil
}
