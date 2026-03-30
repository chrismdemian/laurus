// Package inbox implements the inbox command group.
package inbox

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/cache"
	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/internal/iostreams"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// NewCmdInbox returns the inbox command with subcommands.
func NewCmdInbox(f *cmdutil.Factory) *cobra.Command {
	var opts listOpts

	cmd := &cobra.Command{
		Use:   "inbox",
		Short: "View Canvas inbox conversations",
		Long:  "List conversations, read messages, and check unread count.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return listRun(f, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.Unread, "unread", false, "Show only unread conversations")
	cmd.Flags().BoolVar(&opts.Starred, "starred", false, "Show only starred conversations")
	cmd.Flags().BoolVar(&opts.Sent, "sent", false, "Show sent conversations")
	cmd.Flags().StringVarP(&opts.Course, "course", "c", "", "Filter to a specific course")

	cmd.MarkFlagsMutuallyExclusive("unread", "starred", "sent")

	cmd.AddCommand(newCmdInboxRead(f))
	cmd.AddCommand(newCmdInboxReply(f))
	cmd.AddCommand(newCmdInboxSend(f))
	cmd.AddCommand(newCmdInboxUnreadCount(f))

	return cmd
}

type listOpts struct {
	Unread  bool
	Starred bool
	Sent    bool
	Course  string
}

func listRun(f *cmdutil.Factory, opts listOpts) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	ios := f.IOStreams()
	ctx := context.Background()

	apiOpts := canvas.ListConversationsOptions{}
	if opts.Unread {
		apiOpts.Scope = "unread"
	} else if opts.Starred {
		apiOpts.Scope = "starred"
	} else if opts.Sent {
		apiOpts.Scope = "sent"
	}

	if opts.Course != "" {
		course, err := canvas.FindCourse(ctx, client, opts.Course)
		if err != nil {
			return fmt.Errorf("finding course %q: %w", opts.Course, err)
		}
		apiOpts.Filter = []string{fmt.Sprintf("course_%d", course.ID)}
	}

	var items []canvas.Conversation
	for c, err := range canvas.ListConversations(ctx, client, apiOpts) {
		if err != nil {
			return fmt.Errorf("listing conversations: %w", err)
		}
		items = append(items, c)
	}

	// Opportunistic cache write.
	if db, err := f.Cache(); err == nil {
		cacheItems := make([]cache.CacheItem, len(items))
		for i, x := range items {
			cacheItems[i] = cache.CacheItem{ID: x.ID, CourseID: 0, Data: x}
		}
		_ = db.UpsertMany(cache.ResourceConversations, cacheItems)
		_ = db.SetSyncMeta(cache.ResourceConversations, 0, len(cacheItems), "success")
	}

	if ios.IsJSON {
		return cmdutil.RenderJSON(ios, items)
	}

	if len(items) == 0 {
		_, _ = fmt.Fprintln(ios.Out, "No conversations found.")
		return nil
	}

	palette := cmdutil.NewPalette(ios)
	tbl := cmdutil.NewTable(ios)
	tbl.AddHeader("ID", "SUBJECT", "FROM", "LAST MESSAGE", "DATE", "STATUS")

	for _, c := range items {
		style := palette.Neutral
		status := ""

		if c.WorkflowState == "unread" {
			style = palette.DueToday
			status = "unread"
		}
		if c.Starred {
			if status != "" {
				status += ", "
			}
			status += "starred"
		}

		participants := formatParticipants(c.Participants)
		preview := truncate(c.LastMessage, 50)

		tbl.AddStyledRow(
			cmdutil.StyledCell{Value: strconv.FormatInt(c.ID, 10), Style: style},
			cmdutil.StyledCell{Value: c.Subject, Style: style},
			cmdutil.StyledCell{Value: participants, Style: style},
			cmdutil.StyledCell{Value: preview, Style: style},
			cmdutil.StyledCell{Value: cmdutil.RelativeTime(c.LastMessageAt), Style: style},
			cmdutil.StyledCell{Value: status, Style: style},
		)
	}

	_ = ios.StartPager()
	defer ios.StopPager()
	return tbl.Render()
}

func formatParticipants(participants []canvas.ConversationParticipant) string {
	if len(participants) == 0 {
		return ""
	}
	names := make([]string, 0, len(participants))
	for _, p := range participants {
		names = append(names, p.Name)
	}
	result := strings.Join(names, ", ")
	return truncate(result, 30)
}

func truncate(s string, max int) string {
	// Normalize whitespace (Canvas last_message can have newlines)
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-3]) + "..."
}

func newCmdInboxRead(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "read <id>",
		Short: "Read a conversation",
		Long:  "Show the full message thread for a conversation.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid conversation ID %q: %w", args[0], err)
			}
			return readRun(f, id)
		},
	}
	return cmd
}

func readRun(f *cmdutil.Factory, conversationID int64) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	ios := f.IOStreams()
	ctx := context.Background()

	conv, err := canvas.GetConversation(ctx, client, conversationID)
	if err != nil {
		return fmt.Errorf("fetching conversation: %w", err)
	}

	if ios.IsJSON {
		return cmdutil.RenderJSON(ios, conv)
	}

	return renderConversationDetail(ios, conv)
}

func renderConversationDetail(ios *iostreams.IOStreams, conv canvas.Conversation) error {
	palette := cmdutil.NewPalette(ios)

	_ = ios.StartPager()
	defer ios.StopPager()

	_, _ = fmt.Fprintln(ios.Out, palette.Header.Render(conv.Subject))

	participants := formatParticipants(conv.Participants)
	printField(ios, palette, "Participants", participants)
	printField(ios, palette, "Messages", fmt.Sprintf("%d", conv.MessageCount))

	// Build author lookup
	authorNames := make(map[int64]string)
	for _, p := range conv.Participants {
		authorNames[p.ID] = p.Name
	}

	// Render messages chronologically (Canvas returns newest first, reverse for reading)
	if len(conv.Messages) > 0 {
		_, _ = fmt.Fprintln(ios.Out)

		// Reverse to chronological order
		messages := make([]canvas.ConversationMessage, len(conv.Messages))
		for i, m := range conv.Messages {
			messages[len(conv.Messages)-1-i] = m
		}

		for _, msg := range messages {
			authorName := authorNames[msg.AuthorID]
			if authorName == "" {
				authorName = fmt.Sprintf("User %d", msg.AuthorID)
			}

			_, _ = fmt.Fprintf(ios.Out, "  %s  %s\n",
				palette.Header.Render(authorName),
				palette.Muted.Render(msg.CreatedAt.Format("Jan 2, 3:04 PM")),
			)
			// Body is plain text, not HTML
			_, _ = fmt.Fprintf(ios.Out, "  %s\n\n", msg.Body)
		}
	}

	return nil
}

func newCmdInboxUnreadCount(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unread-count",
		Short: "Show unread conversation count",
		Long:  "Print the number of unread conversations (shell-friendly).",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return unreadCountRun(f)
		},
	}
	return cmd
}

func unreadCountRun(f *cmdutil.Factory) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	ios := f.IOStreams()
	ctx := context.Background()

	count, err := canvas.GetUnreadCount(ctx, client)
	if err != nil {
		return fmt.Errorf("fetching unread count: %w", err)
	}

	if ios.IsJSON {
		return cmdutil.RenderJSON(ios, map[string]int{"unread_count": count})
	}

	_, _ = fmt.Fprintln(ios.Out, count)
	return nil
}

func printField(ios *iostreams.IOStreams, palette *cmdutil.Palette, label, value string) {
	_, _ = fmt.Fprintf(ios.Out, "  %s  %s\n",
		palette.Muted.Render(fmt.Sprintf("%-14s", label)),
		value,
	)
}
