package main

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/twinklehawk/vikunja-client-go"
)

type jobHandler interface {
	canHandleJob(c *jobConfig) bool

	handleJob(c *jobConfig) error
}

// createTaskHandler creates a vikunja task.
type createTaskHandler struct {
	client *vikunja.Client
}

// canHandleJob returns true if the [jobConfig.jobType] is CreateTask.
func (h *createTaskHandler) canHandleJob(c *jobConfig) bool {
	return c.jobType == "CreateTask"
}

// handleJob create a vikunja task according to the parameters in the jobConfig.
func (h *createTaskHandler) handleJob(c *jobConfig) error {
	taskConfig, err := parseCreateTaskConfig(c)
	if err != nil {
		return err
	}

	// find ID for project
	project, err := h.client.FindProjectByTitle(context.TODO(), taskConfig.project)
	if err != nil {
		return fmt.Errorf("finding project: %w", err)
	}
	projectID := project.ID

	t := vikunja.Task{
		Title:       taskConfig.title,
		Description: taskConfig.description,
		ProjectID:   projectID,
	}
	task, err := h.client.CreateTask(context.TODO(), t)
	if err != nil {
		return fmt.Errorf("creating task: %w", err)
	}
	if task.ID == 0 {
		return fmt.Errorf("created task does not have ID set")
	}

	// add labels
	for _, labelTitle := range taskConfig.labels {
		if labelTitle == "" {
			continue
		}
		label, err := h.client.FindLabelByTitle(context.TODO(), labelTitle)
		if err != nil {
			return fmt.Errorf("finding label: %w", err)
		}
		labelID := label.ID

		err = h.client.AddTaskLabel(
			context.TODO(),
			task.ID,
			vikunja.AddTaskLabelRequest{LabelID: labelID},
		)
		if err != nil {
			return fmt.Errorf("adding label '%s': %w", labelTitle, err)
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
	labels      []string
}

func parseCreateTaskConfig(c *jobConfig) (createTaskConfig, error) {
	config := createTaskConfig{
		title:       c.parameters["title"],
		project:     c.parameters["project"],
		description: c.parameters["description"],
		labels:      strings.Split(c.parameters["labels"], ","),
	}
	if config.title == "" || config.project == "" {
		return createTaskConfig{}, fmt.Errorf("task title and project cannot be empty")
	}
	return config, nil
}

// applyTodayLabelHandler manages the Today label on tasks in the Inbox project.
type applyTodayLabelHandler struct {
	client *vikunja.Client
}

// canHandleJob returns true if the [jobConfig.jobType] is ApplyTodayLabel.
func (h *applyTodayLabelHandler) canHandleJob(c *jobConfig) bool {
	return c.jobType == "ApplyTodayLabel"
}

// handleJob applies and removes the Today label on tasks in the Inbox project.
func (h *applyTodayLabelHandler) handleJob(c *jobConfig) error {
	// find ID for inbox project
	project, err := h.client.FindProjectByTitle(context.TODO(), "Inbox")
	if err != nil {
		return fmt.Errorf("finding project: %w", err)
	}
	inboxID := project.ID

	// find ID for priority view in inbox project
	view, err := h.client.FindProjectViewByTitle(context.TODO(), inboxID, "Priority")
	if err != nil {
		return fmt.Errorf("finding project view: %w", err)
	}
	priorityViewID := view.ID

	// find ID for now bucket in view
	bucket, err := h.client.FindProjectViewBucketByTitle(
		context.TODO(),
		inboxID,
		priorityViewID,
		"Now",
	)
	if err != nil {
		return fmt.Errorf("finding bucket in view: %w", err)
	}
	nowBucketID := bucket.ID

	// find ID for today label
	label, err := h.client.FindLabelByTitle(context.TODO(), "Today")
	if err != nil {
		return fmt.Errorf("finding label: %w", err)
	}
	todayLabelID := label.ID

	for task, err := range h.client.ListTasksInProjectViewStream(
		context.TODO(),
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
				context.TODO(),
				task.ID,
				vikunja.AddTaskLabelRequest{LabelID: todayLabelID},
			)
			if err != nil {
				return fmt.Errorf("adding task label: %w", err)
			}
			slog.Info("added today label to task", slog.String("taskTitle", task.Title))
		} else if !taskInBucket && hasLabel {
			err = h.client.RemoveTaskLabel(context.TODO(), task.ID, todayLabelID)
			if err != nil {
				return fmt.Errorf("removing task label: %w", err)
			}
			slog.Info("removed today label from task", slog.String("taskTitle", task.Title))
		}
	}

	return nil
}
