package service

// IsMoshuResellerManaged identifies the product accounts provisioned by the
// reseller enrollment flow. Protocol conversion and upstream account retries
// belong to the main station for these accounts.
func (a *Account) IsMoshuResellerManaged() bool {
	if a == nil || a.Type != AccountTypeAPIKey {
		return false
	}
	managed, _ := a.Extra["moshu_reseller_managed"].(bool)
	return managed
}
