package deployer

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/AlecAivazis/survey/v2"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/briandowns/spinner"
	"github.com/common-fate/clio"
	"github.com/common-fate/cloudform/cfn"
	"github.com/common-fate/cloudform/console"
	"github.com/common-fate/cloudform/ui"
	"github.com/pkg/errors"
)

// Deployer contains methods to interactively
// manage CloudFormations via a CLI.
type Deployer struct {
	cfnClient       *cloudformation.Client
	cloudformClient *cfn.Cfn
	uiClient        *ui.UI
}

// New creates a new
func New(ctx context.Context) (*Deployer, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	return &Deployer{
		cfnClient:       cloudformation.NewFromConfig(cfg),
		cloudformClient: cfn.New(cfg),
		uiClient:        ui.New(cfg),
	}, nil
}

// NewFromConfig creates a Deployer from an existing AWS config.
func NewFromConfig(cfg aws.Config) *Deployer {
	return &Deployer{
		cfnClient:       cloudformation.NewFromConfig(cfg),
		cloudformClient: cfn.New(cfg),
		uiClient:        ui.New(cfg),
	}
}

const noChangeFoundMsg = "The submitted information didn't contain changes. Submit different information to create a change set."

type DeployOpts struct {
	// Template to deploy. Can be either
	// a URL or an inline JSON/YAML string.
	Template string
	// Params are CloudFormation parameters
	Params []types.Parameter
	// Tags to associate with the stack
	Tags map[string]string
	// StackName is the name of the deployed stack
	StackName string
	// RoleARN is an optional deployment role to use
	RoleARN string
	// Confirm will skip interactive confirmations
	// if set to tru
	Confirm bool
}

type DeployOptFunc func(*DeployOpts)

// WithConfirm adjusts the Confirm variable in the deployment options.
func WithConfirm(confirm bool) DeployOptFunc {
	return func(do *DeployOpts) {
		do.Confirm = confirm
	}
}

type DeployResult struct {
	FinalStatus string
}

// Deploy deploys a stack and returns the final status
// template can be either a URL or a template body
func (b *Deployer) Deploy(ctx context.Context, opts DeployOpts) (*DeployResult, error) {
	si := spinner.New(spinner.CharSets[14], 100*time.Millisecond)
	si.Suffix = " creating CloudFormation change set"
	si.Writer = os.Stderr
	si.Start()

	changeSetName, createErr := b.cloudformClient.CreateChangeSet(ctx, opts.Template, opts.Params, opts.Tags, opts.StackName, opts.RoleARN)

	si.Stop()

	if createErr != nil {
		if createErr.Error() == noChangeFoundMsg {
			clio.Info("Skipped deployment (there are no changes in the changeset)")

			res := DeployResult{
				FinalStatus: "DEPLOY_SKIPPED",
			}

			return &res, nil
		}

		return nil, errors.Wrap(createErr, "creating changeset")

	}

	confirm := opts.Confirm

	if !confirm {
		status, err := b.uiClient.FormatChangeSet(ctx, opts.StackName, changeSetName)
		if err != nil {
			return nil, err
		}
		clio.Info("The following CloudFormation changes will be made:")
		fmt.Println(status)

		p := &survey.Confirm{Message: "Do you wish to continue?", Default: true}
		err = survey.AskOne(p, &confirm)
		if err != nil {
			return nil, err
		}
		if !confirm {
			return nil, errors.New("user cancelled deployment")
		}
	}

	err := b.cloudformClient.ExecuteChangeSet(ctx, opts.StackName, changeSetName)
	if err != nil {
		return nil, err
	}

	status, messages := b.uiClient.WaitForStackToSettle(ctx, opts.StackName)

	clio.Infof("Final stack status: %s", ui.ColouriseStatus(status))

	if len(messages) > 0 {
		fmt.Println(console.Yellow("Messages:"))
		for _, message := range messages {
			fmt.Printf("  - %s\n", message)
		}
	}

	res := DeployResult{
		FinalStatus: status,
	}

	return &res, nil
}

type DeleteOpts struct {
	// StackName to delete
	StackName string
	// RoleARN is an optional deployment role to use
	RoleARN string
}

type DeleteResult struct {
	FinalStatus       string
	DeleteStackOutput *cloudformation.DeleteStackOutput
}

// Delete a CloudFormation stack and returns the final status
func (b *Deployer) Delete(ctx context.Context, opts DeleteOpts) (*DeleteResult, error) {
	output, err := b.cloudformClient.DeleteStack(opts.StackName, opts.RoleARN)
	if err != nil {
		return nil, err
	}

	si := spinner.New(spinner.CharSets[14], 100*time.Millisecond)
	si.Suffix = " creating CloudFormation change set"
	si.Writer = os.Stderr
	si.Start()
	si.Stop()

	status, messages := b.uiClient.WaitForStackToSettle(ctx, opts.StackName)

	clio.Infof("Final stack status: %s", ui.ColouriseStatus(status))

	if len(messages) > 0 {
		fmt.Println(console.Yellow("Messages:"))
		for _, message := range messages {
			fmt.Printf("  - %s\n", message)
		}
	}

	res := DeleteResult{
		FinalStatus:       status,
		DeleteStackOutput: output,
	}

	return &res, nil
}
