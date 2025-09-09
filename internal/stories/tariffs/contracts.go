package tariffs

import "context"

type (
	Storage interface {
		CreateTariff(ctx context.Context, tariff Tariff) (*Tariff, error)
		GetTariff(ctx context.Context, criteria GetCriteria) (*Tariff, error)
		UpdateTariff(ctx context.Context, criteria GetCriteria, params UpdateParams) (*Tariff, error)
		ListTariffs(ctx context.Context, criteria ListCriteria) ([]*Tariff, error)
		DeleteTariff(ctx context.Context, criteria DeleteCriteria) error
	}
)