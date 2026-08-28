package tool

type RiskLevel string

const (
	RiskRead  RiskLevel = "read"
	RiskWrite RiskLevel = "write"
	RiskHigh  RiskLevel = "high"
)

func RiskFor(name string) (RiskLevel, bool) {
	switch name {
	case "read_file", "list_dir":
		return RiskRead, true
	case "write_file":
		return RiskWrite, true
	case "shell":
		return RiskHigh, true
	default:
		return "", false
	}
}
