package main

import (
	"time"

	"github.com/juju/cmd"
	"github.com/juju/errors"
	"github.com/juju/juju/api"
	"github.com/juju/juju/api/controller"
	"github.com/juju/juju/api/migrationmaster"
	"github.com/juju/juju/api/watcher"
	"github.com/juju/juju/cmd/modelcmd"
	coremigration "github.com/juju/juju/core/migration"
	"gopkg.in/juju/names.v2"
	"gopkg.in/macaroon.v1"
)

func newExtMigrateCommand() cmd.Command {
	return modelcmd.WrapController(&extMigrateCommand{})
}

// extMigrateCommand initiates a model migration.
type extMigrateCommand struct {
	modelcmd.ControllerCommandBase

	model            string
	targetController string
	machineTag       names.MachineTag
	machinePassword  string
	machineNonce     string
}

type migrateAPI interface {
	InitiateMigration(spec controller.MigrationSpec) (string, error)
}

const migrateDoc = `
Runs an externally controlled migration
`

// Info implements cmd.Command.
func (c *extMigrateCommand) Info() *cmd.Info {
	return &cmd.Info{
		Name:    "migrate",
		Args:    "<model-name> <target-controller-name> <machine-tag> <machine-password> <machine-nonce>",
		Purpose: "Migrate a hosted model to another controller.",
		Doc:     migrateDoc,
	}
}

// Init implements cmd.Command.
func (c *extMigrateCommand) Init(args []string) error {
	if len(args) < 1 {
		return errors.New("model not specified")
	}
	if len(args) < 2 {
		return errors.New("target controller not specified")
	}
	if len(args) < 3 {
		return errors.New("machine tag not specified")
	}
	if len(args) < 4 {
		return errors.New("machine password not specified")
	}
	if len(args) < 5 {
		return errors.New("machine nonce not specified")
	}
	if len(args) > 5 {
		return errors.New("too many arguments specified")
	}

	c.model = args[0]
	c.targetController = args[1]

	// This is just for testing purposes. A real external migration
	// tool would ssh a binary (perhaps itself) over to a controller
	// host and run it after the migration had been triggered. This
	// could then retrieve the controller machine creds from the local
	// agent conf and connect to the source controller itself. It
	// would get the target controller's API details via the
	// MigrationStatus API.
	tag, err := names.ParseMachineTag(args[2])
	if err != nil {
		return err
	}
	c.machineTag = tag
	c.machinePassword = args[3]
	c.machineNonce = args[4]
	return nil
}

func (c *extMigrateCommand) modelUUID() (string, error) {
	modelUUIDs, err := c.ModelUUIDs([]string{c.model})
	if err != nil {
		return "", errors.Trace(err)
	}
	return modelUUIDs[0], nil
}

func (c *extMigrateCommand) getMigrationSpec() (*controller.MigrationSpec, error) {
	store := c.ClientStore()

	modelUUID, err := c.modelUUID()
	if err != nil {
		return nil, err
	}

	controllerInfo, err := store.ControllerByName(c.targetController)
	if err != nil {
		return nil, err
	}

	accountInfo, err := store.AccountDetails(c.targetController)
	if err != nil {
		return nil, err
	}

	var macs []macaroon.Slice
	if accountInfo.Macaroon != "" {
		mac := new(macaroon.Macaroon)
		if err := mac.UnmarshalJSON([]byte(accountInfo.Macaroon)); err != nil {
			return nil, errors.Annotate(err, "unmarshalling macaroon")
		}
		macs = []macaroon.Slice{{mac}}
	}

	return &controller.MigrationSpec{
		ExternalControl:      true,
		ModelUUID:            modelUUID,
		TargetControllerUUID: controllerInfo.ControllerUUID,
		TargetAddrs:          controllerInfo.APIEndpoints,
		TargetCACert:         controllerInfo.CACert,
		TargetUser:           accountInfo.User,
		TargetPassword:       accountInfo.Password,
		TargetMacaroons:      macs,
	}, nil
}

func (c *extMigrateCommand) connectMigrationMaster() (*migrationmaster.Client, error) {
	store := c.ClientStore()
	controllerInfo, err := store.ControllerByName(c.ControllerName())
	if err != nil {
		return nil, err
	}
	modelUUID, err := c.modelUUID()
	if err != nil {
		return nil, err
	}
	apiInfo := &api.Info{
		Addrs:    controllerInfo.APIEndpoints,
		CACert:   controllerInfo.CACert,
		ModelTag: names.NewModelTag(modelUUID),
		Tag:      c.machineTag,
		Password: c.machinePassword,
		Nonce:    c.machineNonce,
	}
	apiConn, err := api.Open(apiInfo, api.DialOpts{})
	if err != nil {
		return nil, err
	}
	return migrationmaster.NewClient(apiConn, watcher.NewNotifyWatcher), nil
}

// Run implements cmd.Command.
func (c *extMigrateCommand) Run(ctx *cmd.Context) error {
	spec, err := c.getMigrationSpec()
	if err != nil {
		return err
	}
	api, err := c.NewControllerAPIClient()
	if err != nil {
		return err
	}
	id, err := api.InitiateMigration(*spec)
	if err != nil {
		return err
	}
	ctx.Infof("Migration started with ID %q", id)

	time.Sleep(5 * time.Second)

	masterClient, err := c.connectMigrationMaster()
	if err != nil {
		return err
	}

	ctx.Infof("Set phase to ABORT")
	err = masterClient.SetPhase(coremigration.ABORT)
	if err != nil {
		return err
	}

	ctx.Infof("Set phase to ABORTDONE")
	err = masterClient.SetPhase(coremigration.ABORTDONE)
	if err != nil {
		return err
	}

	return nil
}
