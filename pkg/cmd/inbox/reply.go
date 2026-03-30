package inbox

import (
	"context"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

func newCmdInboxReply(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reply <conversation-id> <message>",
		Short: "Reply to an inbox conversation",
		Long:  "Add a reply message to an existing Canvas inbox conversation.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return inboxReplyRun(f, args[0], args[1])
		},
	}
	return cmd
}

func inboxReplyRun(f *cmdutil.Factory, idStr, message string) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	ios := f.IOStreams()
	ctx := context.Background()

	conversationID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid conversation ID %q: must be a number", idStr)
	}

	convo, err := canvas.AddConversationMessage(ctx, client, conversationID, message)
	if err != nil {
		return fmt.Errorf("replying to conversation: %w", err)
	}

	_, _ = fmt.Fprintf(ios.Out, "Replied to conversation %q (ID: %d).\n", convo.Subject, convo.ID)
	return nil
}
