package governance

type Version struct {
	SchemaVersion    string
	ParserVersion    string
	ChunkerVersion   string
	MetadataVersion  string
	EmbeddingVersion string
}

func (v Version) String() string {
	return v.SchemaVersion +
		":" +
		v.ParserVersion +
		":" +
		v.ChunkerVersion +
		":" +
		v.MetadataVersion +
		":" +
		v.EmbeddingVersion
}
