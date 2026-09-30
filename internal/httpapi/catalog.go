package httpapi

// Catalog returns the currently migrated session operations in stable order.
func Catalog() []Descriptor {
	return []Descriptor{
		GetScratchpad.Describe(), UpdateScratchpad.Describe(),
		GetSessionVCS.Describe(), StreamSessionVCS.Describe(),
	}
}
