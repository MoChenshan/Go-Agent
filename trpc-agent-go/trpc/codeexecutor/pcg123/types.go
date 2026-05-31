package pcg123

// Language is the language of the code executor
type Language string

const (
	// LanguagePython38 is the python 3.8 language
	LanguagePython38 Language = "python3.8"
	// LanguagePython39 is the python 3.9 language
	LanguagePython39 Language = "python3.9"
	// LanguagePython310 is the python 3.10 language
	LanguagePython310 Language = "python3.10"
)

var validLanguages = []Language{LanguagePython38, LanguagePython39, LanguagePython310}

var skillsEnabledLanguages = []Language{LanguagePython310}

// WorkspaceVolumeMount is the volume mount of the workspace
type WorkspaceVolumeMount struct {
	*CFSVolumeMount // the cfs volume mount of the workspace
}

// CFSVersion is the version of the cfs filesystem protocol
type CFSVersion string

const (
	// CFSVersion3 is the version 3 of the cfs filesystem protocol
	CFSVersion3 CFSVersion = "3"
	// CFSVersion4 is the version 4 of the cfs filesystem protocol
	CFSVersion4 CFSVersion = "4"
)

// CFSVolumeMount is the volume mount of the cfs
// refer to https://iwiki.woa.com/p/1871558973 for more details
type CFSVolumeMount struct {
	Name    string     // the name of the cfs filesystem
	Host    string     // the host of the cfs filesystem
	Path    string     // the path of the cfs filesystem, default start with "/"
	Version CFSVersion // the version of the cfs filesystem protocol
}

func isValidLanguage(language Language) bool {
	for _, l := range validLanguages {
		if l == language {
			return true
		}
	}
	return false
}

func enableSkills(language Language) bool {
	for _, l := range skillsEnabledLanguages {
		if l == language {
			return true
		}
	}
	return false
}

const (
	rpcNameInitExecutor    = "/code_executor/Admin/InitExecutor"
	rpcNameDestroyExecutor = "/code_executor/Admin/DestroyExecutor"
	rpcNameExecuteCode     = "/code_executor/Proxy/ExecuteCode"
)
