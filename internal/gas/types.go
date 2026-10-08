package gas

import "time"

// The wire types, trimmed to the fields used. Names and JSON tags follow
// google.golang.org/api/script/v1 and drive/v3 at v0.301.0; times are parsed
// rather than kept as strings, because every caller compares them.

// DriveFile is a Drive file's metadata. For a standalone script, ID is also
// its script id.
type DriveFile struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	MimeType     string    `json:"mimeType,omitempty"`
	CreatedTime  time.Time `json:"createdTime,omitzero"`
	ModifiedTime time.Time `json:"modifiedTime,omitzero"`
	OwnedByMe    bool      `json:"ownedByMe"`
	Owners       []struct {
		EmailAddress string `json:"emailAddress"`
	} `json:"owners,omitempty"`
	WebViewLink string `json:"webViewLink,omitempty"`
	Trashed     bool   `json:"trashed,omitempty"`
}

// User is the creator or last editor of a project or file.
type User struct {
	Email string `json:"email,omitempty"`
	Name  string `json:"name,omitempty"`
}

// Project is an Apps Script project's metadata. ParentID is set when the
// script is bound to a Docs, Sheets, Forms or Slides file.
type Project struct {
	ScriptID       string    `json:"scriptId"`
	Title          string    `json:"title"`
	ParentID       string    `json:"parentId,omitempty"`
	CreateTime     time.Time `json:"createTime,omitzero"`
	UpdateTime     time.Time `json:"updateTime,omitzero"`
	Creator        *User     `json:"creator,omitempty"`
	LastModifyUser *User     `json:"lastModifyUser,omitempty"`
}

// Content is every file in a project.
type Content struct {
	ScriptID string `json:"scriptId"`
	Files    []File `json:"files"`
}

// File types, as the API names them.
const (
	FileServerJS = "SERVER_JS"
	FileHTML     = "HTML"
	FileJSON     = "JSON" // only ever the manifest, appsscript
)

// File is one file of a project. Name has no extension; Type says what it is.
type File struct {
	Name           string    `json:"name"`
	Type           string    `json:"type"`
	Source         string    `json:"source"`
	CreateTime     time.Time `json:"createTime,omitzero"`
	UpdateTime     time.Time `json:"updateTime,omitzero"`
	LastModifyUser *User     `json:"lastModifyUser,omitempty"`
	FunctionSet    *struct {
		Values []struct {
			Name string `json:"name"`
		} `json:"values"`
	} `json:"functionSet,omitempty"`
}

// Deployment is a versioned (or head) deployment and its entry points.
type Deployment struct {
	DeploymentID     string `json:"deploymentId"`
	DeploymentConfig struct {
		Description   string `json:"description,omitempty"`
		VersionNumber int64  `json:"versionNumber,omitempty"`
	} `json:"deploymentConfig"`
	UpdateTime  time.Time `json:"updateTime,omitzero"`
	EntryPoints []struct {
		EntryPointType string `json:"entryPointType"`
		WebApp         *struct {
			URL              string `json:"url"`
			EntryPointConfig struct {
				Access    string `json:"access"`
				ExecuteAs string `json:"executeAs"`
			} `json:"entryPointConfig"`
		} `json:"webApp,omitempty"`
		ExecutionAPI *struct {
			EntryPointConfig struct {
				Access string `json:"access"`
			} `json:"entryPointConfig"`
		} `json:"executionApi,omitempty"`
	} `json:"entryPoints,omitempty"`
}

// Process is one run of a script. It has no script id; see the package
// comment.
type Process struct {
	ProjectName     string    `json:"projectName"`
	FunctionName    string    `json:"functionName"`
	ProcessType     string    `json:"processType"`
	ProcessStatus   string    `json:"processStatus"`
	UserAccessLevel string    `json:"userAccessLevel,omitempty"`
	StartTime       time.Time `json:"startTime"`
	Duration        string    `json:"duration,omitempty"`
	RuntimeVersion  string    `json:"runtimeVersion,omitempty"`
}

// Metrics are counts over consecutive periods, newest first.
type Metrics struct {
	ActiveUsers      []MetricsValue `json:"activeUsers,omitempty"`
	TotalExecutions  []MetricsValue `json:"totalExecutions,omitempty"`
	FailedExecutions []MetricsValue `json:"failedExecutions,omitempty"`
}

// MetricsValue is one period's count. The API sends the number as a JSON
// string, which is what ",string" undoes.
type MetricsValue struct {
	StartTime time.Time `json:"startTime"`
	EndTime   time.Time `json:"endTime"`
	Value     uint64    `json:"value,omitempty,string"`
}
