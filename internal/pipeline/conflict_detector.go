package pipeline

// ConflictReport describes a file modification conflict between child tickets.
type ConflictReport struct {
	File     string   // the conflicting file path
	Children []string // names of children that all want to modify this file
}

// DetectDecompositionConflicts analyzes FilesToModify across child ticket specs
// and returns a ConflictReport for every file claimed by two or more children.
// Returns an empty slice if there are no conflicts.
func DetectDecompositionConflicts(children []ChildTicketSpec) []ConflictReport {
	fileOwners := make(map[string][]string)
	for _, child := range children {
		for _, file := range child.FilesToModify {
			fileOwners[file] = append(fileOwners[file], child.Title)
		}
	}

	var reports []ConflictReport
	for file, owners := range fileOwners {
		if len(owners) >= 2 {
			reports = append(reports, ConflictReport{
				File:     file,
				Children: owners,
			})
		}
	}
	return reports
}
