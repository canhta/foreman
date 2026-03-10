package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/canhta/foreman/internal/project"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(newProjectCmd())
}

func newProjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage projects",
	}
	cmd.AddCommand(newProjectListCmd())
	cmd.AddCommand(newProjectCreateCmd())
	cmd.AddCommand(newProjectDeleteCmd())
	return cmd
}

func foremanDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".foreman")
}

func newProjectListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all projects",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			mgr := project.NewManager(foremanDir(), cfg)
			entries, err := mgr.ListProjects()
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				fmt.Println("No projects configured. Use 'foreman project create' to add one.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tNAME\tACTIVE\tCREATED")
			for _, e := range entries {
				id := e.ID
				if len(id) > 8 {
					id = id[:8]
				}
				fmt.Fprintf(w, "%s\t%s\t%v\t%s\n", id, e.Name, e.Active, e.CreatedAt.Format("2006-01-02"))
			}
			return w.Flush()
		},
	}
}

func newProjectCreateCmd() *cobra.Command {
	var name, cloneURL, trackerProvider, defaultBranch string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new project",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			mgr := project.NewManager(foremanDir(), cfg)

			projCfg := &project.ProjectConfig{}
			projCfg.Project.Name = name
			projCfg.Git.CloneURL = cloneURL
			projCfg.Git.DefaultBranch = defaultBranch
			projCfg.Tracker.Provider = trackerProvider
			projCfg.AgentRunner.Provider = "builtin"

			id, err := mgr.CreateProject(projCfg)
			if err != nil {
				return err
			}

			shortID := id
			if len(shortID) > 8 {
				shortID = shortID[:8]
			}
			fmt.Printf("Project %q created with ID %s\n", name, shortID)
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Project name (required)")
	cmd.Flags().StringVar(&cloneURL, "clone-url", "", "Git clone URL")
	cmd.Flags().StringVar(&trackerProvider, "tracker", "github", "Tracker provider (github, jira, linear, local_file)")
	cmd.Flags().StringVar(&defaultBranch, "branch", "main", "Default branch")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}

func newProjectDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete [project-id]",
		Short: "Delete a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			mgr := project.NewManager(foremanDir(), cfg)

			if err := mgr.DeleteProject(args[0]); err != nil {
				return err
			}

			fmt.Println("Project deleted.")
			return nil
		},
	}
}
