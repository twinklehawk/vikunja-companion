package main

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/twinklehawk/vikunja-client-go"
)

// jobHandler runs jobs of a single job type. For each job, run selects the first handler whose
// canHandleJob returns true and calls its handleJob.
type jobHandler interface {
	// canHandleJob returns true if this handler can run the job.
	canHandleJob(c *JobConfig) bool

	// handleJob runs the job.
	handleJob(ctx context.Context, c *JobConfig) error
}

// createTaskHandler creates a vikunja task.
type createTaskHandler struct {
	client *vikunja.Client
}

// canHandleJob returns true if the [JobConfig.JobType] is CreateTask.
func (h *createTaskHandler) canHandleJob(c *JobConfig) bool {
	return c.JobType == "CreateTask"
}

// handleJob creates a vikunja task according to the parameters in the JobConfig.
//
// The project and all labels are looked up before the task is created so that a missing project
// or label does not leave behind a task without its labels.
func (h *createTaskHandler) handleJob(ctx context.Context, c *JobConfig) error {
	taskConfig, err := parseCreateTaskConfig(c)
	if err != nil {
		return err
	}

	// find ID for project
	project, err := h.client.FindProjectByTitle(ctx, taskConfig.project)
	if err != nil {
		return fmt.Errorf("finding project: %w", err)
	}

	// find IDs for labels
	labelIDs := make([]int64, 0, len(taskConfig.labels))
	for _, labelTitle := range taskConfig.labels {
		label, err := h.client.FindLabelByTitle(ctx, labelTitle)
		if err != nil {
			return fmt.Errorf("finding label '%s': %w", labelTitle, err)
		}
		labelIDs = append(labelIDs, label.ID)
	}

	t := vikunja.Task{
		Title:       taskConfig.title,
		Description: taskConfig.description,
		ProjectID:   project.ID,
	}
	task, err := h.client.CreateTask(ctx, t)
	if err != nil {
		return fmt.Errorf("creating task: %w", err)
	}
	if task.ID == 0 {
		return fmt.Errorf("created task does not have ID set")
	}

	// add labels
	for i, labelID := range labelIDs {
		err = h.client.AddTaskLabel(
			ctx,
			task.ID,
			vikunja.AddTaskLabelRequest{LabelID: labelID},
		)
		if err != nil {
			return fmt.Errorf("adding label '%s': %w", taskConfig.labels[i], err)
		}
	}

	slog.Info("created task", slog.String("task", task.Title))

	return nil
}

// createTaskConfig contains configuration for the task to be created.
type createTaskConfig struct {
	title       string
	project     string
	description string
	// labels are the titles of the labels to add to the task. Titles are trimmed, non-empty and
	// unique.
	labels []string
}

// parseCreateTaskConfig reads a [createTaskConfig] from the job parameters. The labels parameter
// is a comma-separated list of label titles.
func parseCreateTaskConfig(c *JobConfig) (createTaskConfig, error) {
	config := createTaskConfig{
		title:       c.Parameters["title"],
		project:     c.Parameters["project"],
		description: c.Parameters["description"],
		labels:      parseLabels(c.Parameters["labels"]),
	}
	if config.title == "" || config.project == "" {
		return createTaskConfig{}, fmt.Errorf("task title and project cannot be empty")
	}
	return config, nil
}

// parseLabels splits a comma-separated list of label titles, trimming whitespace and dropping
// empty and duplicate titles. The order of first occurrence is kept.
func parseLabels(value string) []string {
	var labels []string
	for label := range strings.SplitSeq(value, ",") {
		label = strings.TrimSpace(label)
		if label != "" && !slices.Contains(labels, label) {
			labels = append(labels, label)
		}
	}
	return labels
}

// applyTodayLabelHandler manages the Today label on tasks in the Inbox project.
type applyTodayLabelHandler struct {
	client *vikunja.Client
}

// canHandleJob returns true if the [JobConfig.JobType] is ApplyTodayLabel.
func (h *applyTodayLabelHandler) canHandleJob(c *JobConfig) bool {
	return c.JobType == "ApplyTodayLabel"
}

// handleJob applies and removes the Today label on tasks in the Inbox project.
func (h *applyTodayLabelHandler) handleJob(ctx context.Context, c *JobConfig) error {
	// find ID for inbox project
	project, err := h.client.FindProjectByTitle(ctx, "Inbox")
	if err != nil {
		return fmt.Errorf("finding project: %w", err)
	}
	inboxID := project.ID

	// find ID for priority view in inbox project
	view, err := h.client.FindProjectViewByTitle(ctx, inboxID, "Priority")
	if err != nil {
		return fmt.Errorf("finding project view: %w", err)
	}
	priorityViewID := view.ID

	// find ID for now bucket in view
	bucket, err := h.client.FindProjectViewBucketByTitle(
		ctx,
		inboxID,
		priorityViewID,
		"Now",
	)
	if err != nil {
		return fmt.Errorf("finding bucket in view: %w", err)
	}
	nowBucketID := bucket.ID

	// find ID for today label
	label, err := h.client.FindLabelByTitle(ctx, "Today")
	if err != nil {
		return fmt.Errorf("finding label: %w", err)
	}
	todayLabelID := label.ID

	for task, err := range h.client.ListTasksInProjectViewStream(
		ctx,
		inboxID,
		priorityViewID,
		vikunja.TaskListQuery{Expand: []vikunja.TaskExpand{vikunja.TaskExpandBuckets}},
	) {
		if err != nil {
			return fmt.Errorf("iterating through tasks in project view: %w", err)
		}
		taskInBucket := slices.ContainsFunc(
			task.Buckets,
			func(b vikunja.Bucket) bool { return b.ID == nowBucketID && b.ProjectViewID == priorityViewID },
		)
		hasLabel := slices.ContainsFunc(
			task.Labels,
			func(label vikunja.Label) bool { return label.ID == todayLabelID },
		)
		if taskInBucket && !hasLabel {
			err = h.client.AddTaskLabel(
				ctx,
				task.ID,
				vikunja.AddTaskLabelRequest{LabelID: todayLabelID},
			)
			if err != nil {
				return fmt.Errorf("adding task label: %w", err)
			}
			slog.Info("added today label to task", slog.String("taskTitle", task.Title))
		} else if !taskInBucket && hasLabel {
			err = h.client.RemoveTaskLabel(ctx, task.ID, todayLabelID)
			if err != nil {
				return fmt.Errorf("removing task label: %w", err)
			}
			slog.Info("removed today label from task", slog.String("taskTitle", task.Title))
		}
	}

	return nil
}
