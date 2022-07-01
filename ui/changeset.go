package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/aws/smithy-go/ptr"
	"github.com/common-fate/cloudform/console"
)

func (u *UI) FormatChangeSet(ctx context.Context, stackName, changeSetName string) (string, error) {
	status, err := u.cfnClient.GetChangeSet(ctx, stackName, changeSetName)
	if err != nil {
		return "", Errorf(err, "error getting changeset '%s' for stack '%s'", changeSetName, stackName)
	}

	out := strings.Builder{}

	out.WriteString(fmt.Sprintf("%s:\n", console.Yellow(fmt.Sprintf("Stack %s", ptr.ToString(status.StackName)))))

	// Non-stack resources
	for _, change := range status.Changes {
		if change.ResourceChange.ChangeSetId != nil {
			// Bunch up nested stacks to the end
			continue
		}

		line := fmt.Sprintf("%s %s",
			*change.ResourceChange.ResourceType,
			*change.ResourceChange.LogicalResourceId,
		)

		switch change.ResourceChange.Action {
		case types.ChangeAction("Add"):
			out.WriteString(console.Green("  + " + line))
		case types.ChangeAction("Modify"):
			out.WriteString(console.Blue("  > " + line))
		case types.ChangeAction("Remove"):
			out.WriteString(console.Red("  - " + line))
		}

		out.WriteString("\n")
	}

	// Nested stacks
	for _, change := range status.Changes {
		if change.ResourceChange.ChangeSetId == nil {
			continue
		}

		child, err := u.FormatChangeSet(ctx, "", ptr.ToString(change.ResourceChange.ChangeSetId))
		if err != nil {
			return "", err
		}
		parts := strings.SplitN(child, "\n", 2)
		header, body := parts[0], parts[1]

		switch change.ResourceChange.Action {
		case types.ChangeAction("Add"):
			out.WriteString(console.Green("  + " + header))
		case types.ChangeAction("Modify"):
			out.WriteString(console.Blue("  > " + header))
		case types.ChangeAction("Remove"):
			out.WriteString(console.Red("  - " + header))
		}
		out.WriteString("\n")

		out.WriteString(Indent("  ", body))
		out.WriteString("\n")
	}

	return strings.TrimSpace(out.String()), nil
}
