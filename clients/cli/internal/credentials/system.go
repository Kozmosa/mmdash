package credentials

type systemStore struct{}

func NewSystemStore() Store { return systemStore{} }

func (systemStore) Get(profile string) (Credential, error) {
	value, err := platformGet(profile)
	if err != nil {
		return Credential{}, err
	}
	return decode(value)
}

func (systemStore) Set(profile string, credential Credential) error {
	value, err := encode(credential)
	if err != nil {
		return err
	}
	return platformSet(profile, value)
}

func (systemStore) Delete(profile string) error { return platformDelete(profile) }
