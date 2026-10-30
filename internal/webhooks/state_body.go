package webhooks

import "context"

type stateData struct {
	Account      string `json:"account"`
	AccountPhone string `json:"account_phone"`
	State        string `json:"state"`
}

func (d *Dispatcher) stateBody(ctx context.Context, a attempt) (Body, error) {
	state, err := d.journaledState(ctx, a)
	if err != nil {
		return Body{}, err
	}
	return newBody(EventAccountState, a.at, stateData{Account: a.account.Name, AccountPhone: a.account.Phone, State: state}), nil
}

func (d *Dispatcher) journaledState(ctx context.Context, a attempt) (string, error) {
	journal, err := d.deps.Store.DeliveryEvents(ctx, a.delivery)
	if err != nil || len(journal) == 0 {
		return a.account.State, err
	}
	return journal[0].State, nil
}
