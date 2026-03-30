package discussions

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// NewCmdReply returns the reply command for posting discussion entries.
func NewCmdReply(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reply <course> <topic> <message>",
		Short: "Post a reply to a discussion topic",
		Long:  "Post a new top-level reply to a discussion topic in a course.",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return replyRun(f, args[0], args[1], args[2])
		},
	}
	return cmd
}

func replyRun(f *cmdutil.Factory, courseQuery, topicQuery, message string) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	ios := f.IOStreams()
	ctx := context.Background()

	course, err := canvas.FindCourse(ctx, client, courseQuery)
	if err != nil {
		return fmt.Errorf("finding course %q: %w", courseQuery, err)
	}

	topic, err := canvas.FindDiscussionTopic(ctx, client, course.ID, topicQuery)
	if err != nil {
		return fmt.Errorf("finding topic %q: %w", topicQuery, err)
	}

	if topic.Locked {
		return fmt.Errorf("topic %q is locked and does not accept replies", topic.Title)
	}

	_, err = canvas.CreateDiscussionEntry(ctx, client, course.ID, topic.ID, message)
	if err != nil {
		return fmt.Errorf("posting reply: %w", err)
	}

	_, _ = fmt.Fprintf(ios.Out, "Replied to %q in %s.\n", topic.Title, course.CourseCode)
	return nil
}
