package keeper

import (
	"github.com/pokt-network/pocket-core/x/apps/exported"
	"github.com/pokt-network/pocket-core/x/apps/types"
	sdk "github.com/pokt-network/posmint/types"
)

// Application - wrapper for GetApplication call
func (k Keeper) Application(ctx sdk.Ctx, address sdk.Address) exported.ApplicationI {
	app, found := k.GetApplication(ctx, address)
	if !found {
		return nil
	}
	return app
}

// AllApplications - Retrieve a list of all applications
func (k Keeper) AllApplications(ctx sdk.Ctx) (apps []exported.ApplicationI) {
	store := ctx.KVStore(k.storeKey)
	iterator := sdk.KVStorePrefixIterator(store, types.AllApplicationsKey)
	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		app, err := types.UnmarshalApplication(k.cdc, iterator.Value())
		if err != nil {
			k.Logger(ctx).Error("couldn't unmarshal application in AllApplications call: " + string(iterator.Value()) + "\n" + err.Error())
			continue
		}
		apps = append(apps, app)
	}
	return apps
}
