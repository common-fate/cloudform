package cfn

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/aws/smithy-go"
	"github.com/aws/smithy-go/ptr"
)

type Cfn struct {
	client *cloudformation.Client
}

func New(cfg aws.Config) *Cfn {
	client := cloudformation.NewFromConfig(cfg)
	return &Cfn{client}
}

// GetStack returns a cloudformation.Stack representing the named stack
func (c *Cfn) GetStack(ctx context.Context, stackName string) (types.Stack, error) {
	// Get the stack properties
	res, err := c.client.DescribeStacks(ctx, &cloudformation.DescribeStacksInput{
		StackName: &stackName,
	})
	var snf *types.StackNotFoundException
	if errors.As(err, &snf) {
		return types.Stack{}, ErrStackNotExist
	}

	var ve *smithy.GenericAPIError
	if err != nil && !errors.As(err, &ve) {
		return types.Stack{}, err
	}
	if ve != nil && ve.Code == "ValidationError" {
		return types.Stack{}, ErrStackNotExist
	}

	return res.Stacks[0], nil
}

var ErrStackNotExist = errors.New("stack does not exist")

// GetStackResources returns a list of the resources in the named stack
func (c *Cfn) GetStackResources(ctx context.Context, stackName string) ([]types.StackResource, error) {
	// Get the stack resources
	res, err := c.client.DescribeStackResources(ctx, &cloudformation.DescribeStackResourcesInput{
		StackName: &stackName,
	})
	if err != nil {
		return nil, err
	}

	return res.StackResources, nil
}

// GetChangeSet returns the named changeset
func (c *Cfn) GetChangeSet(ctx context.Context, stackName, changeSetName string) (*cloudformation.DescribeChangeSetOutput, error) {
	input := &cloudformation.DescribeChangeSetInput{
		ChangeSetName: aws.String(changeSetName),
	}

	// Stack name is optional
	if stackName != "" {
		input.StackName = aws.String(stackName)
	}

	return c.client.DescribeChangeSet(ctx, input)
}

// CreateChangeSet creates a changeset
// template can be either a URL or a template body
func (c *Cfn) CreateChangeSet(ctx context.Context, template string, params []types.Parameter, tags map[string]string, stackName string, roleArn string) (string, error) {

	changeSetType := "CREATE"

	existingStack, err := c.GetStack(ctx, stackName)
	if err != nil && err != ErrStackNotExist {
		return "", err
	}

	if existingStack.StackId != nil {
		changeSetType = "UPDATE"
	}

	var templateBody *string
	var templateURL *string

	u, err := url.Parse(template)
	if err == nil {
		templateURL = aws.String(u.String())
	} else {
		templateBody = &template
	}

	changeSetName := stackName + "-" + fmt.Sprint(time.Now().Unix())

	input := &cloudformation.CreateChangeSetInput{
		ChangeSetType:       types.ChangeSetType(changeSetType),
		ChangeSetName:       ptr.String(changeSetName),
		StackName:           ptr.String(stackName),
		Tags:                makeTags(tags),
		IncludeNestedStacks: ptr.Bool(true),
		Parameters:          params,
		TemplateURL:         templateURL,
		TemplateBody:        templateBody,
		Capabilities: []types.Capability{
			"CAPABILITY_NAMED_IAM",
			"CAPABILITY_AUTO_EXPAND",
		},
	}

	if roleArn != "" {
		input.RoleARN = ptr.String(roleArn)
	}

	_, err = c.client.CreateChangeSet(ctx, input)
	if err != nil {
		return changeSetName, err
	}

	for {
		res, err := c.client.DescribeChangeSet(ctx, &cloudformation.DescribeChangeSetInput{
			ChangeSetName: &changeSetName,
			StackName:     &stackName,
		})
		if err != nil {
			return changeSetName, err
		}

		status := string(res.Status)

		if status == "FAILED" {
			return changeSetName, errors.New(ptr.ToString(res.StatusReason))
		}

		if strings.HasSuffix(status, "_COMPLETE") {
			break
		}

		time.Sleep(time.Second * 2)
	}

	return changeSetName, nil
}

// ExecuteChangeSet executes the named changeset
func (c *Cfn) ExecuteChangeSet(ctx context.Context, stackName, changeSetName string) error {
	_, err := c.client.ExecuteChangeSet(ctx, &cloudformation.ExecuteChangeSetInput{
		ChangeSetName: &changeSetName,
		StackName:     &stackName,
	})

	return err
}

// DeleteStack deletes a stack
func (c *Cfn) DeleteStack(stackName string, roleArn string) (*cloudformation.DeleteStackOutput, error) {
	input := &cloudformation.DeleteStackInput{
		StackName: &stackName,
	}

	// roleArn is optional
	if roleArn != "" {
		input.RoleARN = ptr.String(roleArn)
	}

	return c.client.DeleteStack(context.Background(), input)
}

func makeTags(tags map[string]string) []types.Tag {
	out := make([]types.Tag, 0)

	for key, value := range tags {
		out = append(out, types.Tag{
			Key:   ptr.String(key),
			Value: ptr.String(value),
		})
	}

	return out
}
