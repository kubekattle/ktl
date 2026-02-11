package secrets

type ApplyTo string

const (
	ApplyBuildArgName  ApplyTo = "build_arg_name"
	ApplyBuildArgValue ApplyTo = "build_arg_value"
	ApplyOCIPath       ApplyTo = "oci_path"
	ApplyOCIContent    ApplyTo = "oci_content"
	ApplyLogLine       ApplyTo = "log_line"
)

type Rule struct {
	ID        string    `json:"id" yaml:"id"`
	Enabled   *bool     `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Severity  Severity  `json:"severity" yaml:"severity"`
	AppliesTo []ApplyTo `json:"applies_to,omitempty" yaml:"applies_to,omitempty"`
	Regex     string    `json:"regex,omitempty" yaml:"regex,omitempty"`
	Message   string    `json:"message,omitempty" yaml:"message,omitempty"`
	Suggest   string    `json:"suggestion,omitempty" yaml:"suggestion,omitempty"`
}

type Config struct {
	Version string `json:"version" yaml:"version"`
	Rules   []Rule `json:"rules" yaml:"rules"`
}
