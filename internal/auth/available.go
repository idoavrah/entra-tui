package auth

// AzureCLIAvailable reports whether the Azure CLI is installed and holds a
// usable Graph token. The login screen uses it to preselect the option that
// will not require a browser round trip.
//
// It is a probe, not a commitment: a true result still goes through the
// normal provider construction when the user confirms.
func AzureCLIAvailable() bool {
	_, err := azBinary()
	return err == nil
}
