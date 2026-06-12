package tenant

import (
	"context"
	"time"

	"encore.dev"
)

func init() {
	if encore.Meta().Environment.Cloud == encore.CloudLocal {
		return
	}
	go func() {
		time.Sleep(3 * time.Second)
		repairAllCloudSchemaDeployGrants(context.Background())
	}()
}
