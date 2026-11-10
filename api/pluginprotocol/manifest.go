package pluginprotocol

// Manifest is a plugin's launch document, plugin.json in its installation
// directory. A manifest declaring ProtocolVersion 2 speaks this protocol;
// one without it speaks version 1.
type Manifest struct {
	Schema          string         `json:"$schema,omitempty" schema:"format=uri" doc:"Absolute URI of the manifest schema, for editors. Kit does not fetch it."`
	ManifestVersion int            `json:"manifestVersion" schema:"const=1"`
	ProtocolVersion int            `json:"protocolVersion,omitempty" schema:"const=2,required" doc:"The plugin protocol version the plugin speaks."`
	ID              string         `json:"id" schema:"def=PluginID"`
	Name            string         `json:"name,omitempty" schema:"minLength=1,maxLength=128" doc:"Display name."`
	Transport       StdioTransport `json:"transport"`
}

// StdioTransport launches a command directly, without a shell. A bare command
// is resolved through PATH; a command containing a path separator is resolved
// relative to the manifest directory. Arguments are passed literally.
type StdioTransport struct {
	Type    string   `json:"type" schema:"const=stdio"`
	Command string   `json:"command" schema:"minLength=1"`
	Args    []string `json:"args,omitempty"`
}
