package inbox

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

func newCmdInboxSend(f *cmdutil.Factory) *cobra.Command {
	var course string

	cmd := &cobra.Command{
		Use:   "send <recipient> <subject> <body>",
		Short: "Send a new inbox message",
		Long: `Send a new message to a Canvas user.

The recipient is resolved by fuzzy name search. Use --course to limit
the recipient search to a specific course context.`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendRun(f, args[0], args[1], args[2], course)
		},
	}

	cmd.Flags().StringVarP(&course, "course", "c", "", "Course context for recipient search")

	return cmd
}

func sendRun(f *cmdutil.Factory, recipientQuery, subject, body, courseQuery string) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	ios := f.IOStreams()
	ctx := context.Background()

	// Resolve course context if provided
	var contextCode string
	if courseQuery != "" {
		course, err := canvas.FindCourse(ctx, client, courseQuery)
		if err != nil {
			return fmt.Errorf("finding course %q: %w", courseQuery, err)
		}
		contextCode = fmt.Sprintf("course_%d", course.ID)
	}

	// Find recipient
	recipient, err := canvas.FindRecipient(ctx, client, recipientQuery, contextCode)
	if err != nil {
		return fmt.Errorf("finding recipient %q: %w", recipientQuery, err)
	}

	req := canvas.CreateConversationRequest{
		Recipients:  []string{recipient.ID},
		Subject:     subject,
		Body:        body,
		ContextCode: contextCode,
	}

	convos, err := canvas.CreateConversation(ctx, client, req)
	if err != nil {
		return fmt.Errorf("sending message: %w", err)
	}

	if len(convos) > 0 {
		_, _ = fmt.Fprintf(ios.Out, "Sent message to %s (conversation %d).\n", recipient.Name, convos[0].ID)
	} else {
		_, _ = fmt.Fprintf(ios.Out, "Sent message to %s.\n", recipient.Name)
	}
	return nil
}
