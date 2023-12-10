package nrpc

import (
	"golang.org/x/exp/maps"
	"log/slog"
	"net"
	"net/http"
	"net/rpc"
	"nqk/internal"
	"os"
	"path"
	"syscall"
)

type NqkRpcService struct {
	record        internal.StateRecord
	actionChannel chan internal.DaemonCommand
}

func ref[T interface{}](v T) *T {
	return &v
}

// func ForceApply() nil

func (t *NqkRpcService) ForceApply(args *interface{}, result *interface{}) error {
	t.actionChannel <- internal.CommandApply
	return nil
}

func ForceApply(rpc *rpc.Client) error {
	var reply interface{}
	return rpc.Call("NqkRpcService.ForceApply", &reply, &reply)
}

// func GetAllStatus() []ActiveProjectState

type GetAllStatusArgs struct{}

type GetAllStatusResult []internal.ActiveProjectState

func getAllStatusImpl(context NqkRpcService) []internal.ActiveProjectState {
	//context.record.Lock.Lock()
	values := maps.Values(context.record.Projects)
	//context.record.Lock.Unlock()
	v := make([]internal.ActiveProjectState, len(values))
	for i := 0; i < len(values); i++ {
		v[i] = *values[i]
	}
	return v
}

func (t *NqkRpcService) GetAllStatus(args *GetAllStatusArgs, result *GetAllStatusResult) error {
	*result = getAllStatusImpl(*t)
	return nil
}

func GetAllStatus(rpc *rpc.Client) (GetAllStatusResult, error) {
	var reply GetAllStatusResult
	err := rpc.Call("NqkRpcService.GetAllStatus", &GetAllStatusArgs{}, &reply)
	if err != nil {
		return nil, err
	}

	return reply, nil
}

//-------------

func Bind(service NqkRpcService) error {
	err := rpc.Register(&service)
	if err != nil {
		return err
	}

	return nil
}

func Launch(record internal.StateRecord, channel chan internal.DaemonCommand) error {
	err := Bind(NqkRpcService{
		record:        record,
		actionChannel: channel,
	})
	if err != nil {
		return err
	}

	rpc.HandleHTTP()

	socket := ""
	if v, ok := os.LookupEnv("RUNTIME_DIRECTORY"); ok {
		socket = path.Join(v, "nqkd.sock")
	} else {
		socket = "nqkd.sock"
	}

	slog.Info("Using socket file", "socket", socket)

	syscall.Unlink(socket)
	unixListener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	err = os.Chmod(socket, 0777)
	if err != nil {
		slog.Error("Failed to set permissions on socket file - client may not work")
	}

	defer unixListener.Close()

	err = http.Serve(unixListener, nil)
	if err != nil {
		return err
	}

	return nil
}
