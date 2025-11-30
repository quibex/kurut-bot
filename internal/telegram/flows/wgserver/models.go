package wgserver

const (
	StateListServers       = "wgserver_list"
	StateAddServer         = "wgserver_add_start"
	StateAddName           = "wgserver_add_name"
	StateAddEndpoint       = "wgserver_add_endpoint"
	StateAddGRPCAddr       = "wgserver_add_grpc"
	StateAddHealthEndpoint = "wgserver_add_health"
	StateAddMaxPeers       = "wgserver_add_maxpeers"
	StateEditServer        = "wgserver_edit"
	StateDisableServer     = "wgserver_disable"
	StateEnableServer      = "wgserver_enable"
)

type AddServerData struct {
	Name           string
	Endpoint       string
	GRPCAddress    string
	HealthEndpoint string
	MaxPeers       int
	MessageID      *int
}
