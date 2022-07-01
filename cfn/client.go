package cfn

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/aws/smithy-go"
	"github.com/aws/smithy-go/ptr"
)

func getClient() (*cloudformation.Client, error) {
	ctx := context.TODO()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	client := cloudformation.NewFromConfig(cfg)
	return client, nil
}

// GetStack returns a cloudformation.Stack representing the named stack
func GetStack(stackName string) (types.Stack, error) {
	client, err := getClient()
	if err != nil {
		return types.Stack{}, err
	}

	// Get the stack properties
	res, err := client.DescribeStacks(context.Background(), &cloudformation.DescribeStacksInput{
		StackName: &stackName,
	})
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
func GetStackResources(stackName string) ([]types.StackResource, error) {
	client, err := getClient()
	if err != nil {
		return nil, err
	}

	// Get the stack resources
	res, err := client.DescribeStackResources(context.Background(), &cloudformation.DescribeStackResourcesInput{
		StackName: &stackName,
	})
	if err != nil {
		return nil, err
	}

	return res.StackResources, nil
}

// GetChangeSet returns the named changeset
func GetChangeSet(stackName, changeSetName string) (*cloudformation.DescribeChangeSetOutput, error) {
	client, err := getClient()
	if err != nil {
		return nil, err
	}
	input := &cloudformation.DescribeChangeSetInput{
		ChangeSetName: aws.String(changeSetName),
	}

	// Stack name is optional
	if stackName != "" {
		input.StackName = aws.String(stackName)
	}

	return client.DescribeChangeSet(context.Background(), input)
}

// CreateChangeSet creates a changeset
func CreateChangeSet(templateURL string, params []types.Parameter, tags map[string]string, stackName string, roleArn string) (string, error) {

	changeSetType := "CREATE"

	existingStack, err := GetStack(stackName)
	if err != nil && err != ErrStackNotExist {
		return "", err
	}

	if existingStack.StackId != nil {
		changeSetType = "UPDATE"
	}

	changeSetName := stackName + "-" + fmt.Sprint(time.Now().Unix())

	input := &cloudformation.CreateChangeSetInput{
		ChangeSetType:       types.ChangeSetType(changeSetType),
		ChangeSetName:       ptr.String(changeSetName),
		StackName:           ptr.String(stackName),
		Tags:                makeTags(tags),
		IncludeNestedStacks: ptr.Bool(true),
		Parameters:          params,
		TemplateURL:         &templateURL,
		Capabilities: []types.Capability{
			"CAPABILITY_NAMED_IAM",
			"CAPABILITY_AUTO_EXPAND",
		},
	}

	if roleArn != "" {
		input.RoleARN = ptr.String(roleArn)
	}
	client, err := getClient()
	if err != nil {
		return changeSetName, err
	}

	_, err = client.CreateChangeSet(context.Background(), input)
	if err != nil {
		return changeSetName, err
	}

	for {
		res, err := client.DescribeChangeSet(context.Background(), &cloudformation.DescribeChangeSetInput{
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
func ExecuteChangeSet(stackName, changeSetName string) error {
	client, err := getClient()
	if err != nil {
		return err
	}

	_, err = client.ExecuteChangeSet(context.Background(), &cloudformation.ExecuteChangeSetInput{
		ChangeSetName: &changeSetName,
		StackName:     &stackName,
	})

	return err
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
