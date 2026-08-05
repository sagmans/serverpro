package config

func (c Config) Validate() error {
	c.normalizeNamespace()
	for _, validate := range []func() error{
		c.validateRequired,
		c.validateResourceNames,
		c.validateProjectScopes,
		c.validateMVPConstraints,
		c.validateAdminUsername,
	} {
		if err := validate(); err != nil {
			return err
		}
	}
	return nil
}
