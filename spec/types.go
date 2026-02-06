package spec

// SessionID identifies a skills runtime session (UUIDv7 string).
type SessionID string

// SkillRecord is the registry record for a skill package (per spec).
type SkillRecord struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Location    string         `json:"location,omitempty"` // abs path to SKILL.md (optional for tool-only agents)
	RootDir     string         `json:"root_dir"`
	Properties  map[string]any `json:"properties,omitempty"`

	// SkillMDBody is cached post-load for prompt injection. Omitted when only metadata is indexed.
	SkillMDBody string `json:"skill_md_body,omitempty"`

	// Digest is implementation-defined. This runtime uses "sha256:<hex>" over SKILL.md bytes.
	Digest string `json:"digest,omitempty"`
}

// LoadMode - Spec recommends "replace" as default.
type LoadMode string

const (
	LoadModeReplace LoadMode = "replace"
	LoadModeAdd     LoadMode = "add"
)

type LoadArgs struct {
	Names []string `json:"names"`
	Mode  LoadMode `json:"mode,omitempty"` // default: replace
}

type UnloadArgs struct {
	Names []string `json:"names,omitempty"`
	All   bool     `json:"all,omitempty"`
}

type ReadEncoding string

const (
	ReadEncodingText   ReadEncoding = "text"
	ReadEncodingBinary ReadEncoding = "binary"
)

type ReadArgs struct {
	// If empty, uses most recently loaded active skill.
	Skill string `json:"skill,omitempty"`

	// Relative path under the selected skill root.
	Path string `json:"path"`

	Encoding ReadEncoding `json:"encoding,omitempty"` // default: text
}

type RunScriptArgs struct {
	// If empty, uses most recently loaded active skill.
	Skill string `json:"skill,omitempty"`

	// Relative path under the selected skill root.
	// MUST be under scripts/ (enforced by runtime).
	Path string `json:"path"`

	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Workdir string            `json:"workdir,omitempty"` // relative under skill root; default "."
}

type SkillRef struct {
	Name       string         `json:"name"`
	Location   string         `json:"location,omitempty"`
	RootDir    string         `json:"root_dir"`
	Digest     string         `json:"digest,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
}

type LoadResult struct {
	ActiveSkills []SkillRef `json:"active_skills"`
}

type UnloadResult struct {
	ActiveSkills []SkillRef `json:"active_skills"`
}

type RunScriptResult struct {
	Path       string `json:"path"`
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	TimedOut   bool   `json:"timed_out,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
}
