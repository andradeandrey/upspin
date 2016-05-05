// Package remote implements an inprocess directory server that uses RPC to
// connect to a remote directory server.
package remote

import (
	"errors"
	"fmt"
	"net/rpc"
	"strings"
	"sync"

	"upspin.googlesource.com/upspin.git/bind"
	"upspin.googlesource.com/upspin.git/directory/proto"
	"upspin.googlesource.com/upspin.git/upspin"
)

// remote implements upspin.Directory.
type remote struct {
	upspin.NoConfiguration
	endpoint  upspin.Endpoint
	userName  upspin.UserName
	id        int
	rpcClient *rpc.Client
}

// connections contains a list of all extant connections.
var connections struct {
	sync.Mutex
	c []*remote
}

var _ upspin.Directory = (*remote)(nil)

// call calls the RPC method for the user associated with the remote.
func (r *remote) call(method string, req, resp interface{}) error {
	return r.rpcClient.Call(fmt.Sprintf("Server_%d.%s", r.id, method), req, resp)
}

// Glob implements upspin.Directory.Glob.
func (r *remote) Glob(pattern string) ([]*upspin.DirEntry, error) {
	req := &proto.GlobRequest{
		Pattern: pattern,
	}
	var resp proto.GlobResponse
	err := r.call("Glob", &req, &resp)
	return resp.Entries, err
}

// MakeDirectory implements upspin.Directory.MakeDirectory.
func (r *remote) MakeDirectory(directoryName upspin.PathName) (upspin.Location, error) {
	req := &proto.MakeDirectoryRequest{
		Name: directoryName,
	}
	var resp proto.MakeDirectoryResponse
	err := r.call("MakeDirectory", &req, &resp)
	return resp.Location, err
}

// Put implements upspin.Directory.Put.
// Directories are created with MakeDirectory. Roots are anyway. TODO?.
func (r *remote) Put(entry *upspin.DirEntry) error {
	req := &proto.PutRequest{
		Entry: entry,
	}
	var resp proto.PutResponse
	return r.call("Put", &req, &resp)
}

// WhichAccess implements upspin.Directory.WhichAccess.
func (r *remote) WhichAccess(pathName upspin.PathName) (upspin.PathName, error) {
	req := &proto.WhichAccessRequest{
		Name: pathName,
	}
	var resp proto.WhichAccessResponse
	err := r.call("WhichAccess", &req, &resp)
	return resp.Name, err
}

// Delete implements upspin.Directory.Delete.
func (r *remote) Delete(pathName upspin.PathName) error {
	req := &proto.DeleteRequest{
		Name: pathName,
	}
	var resp proto.DeleteResponse
	return r.call("Delete", &req, &resp)
}

// Lookup implements upspin.Directory.Lookup.
func (r *remote) Lookup(pathName upspin.PathName) (*upspin.DirEntry, error) {
	req := &proto.LookupRequest{
		Name: pathName,
	}
	var resp proto.LookupResponse
	err := r.call("Lookup", &req, &resp)
	return resp.Entry, err
}

// Authenticate tells the server which user this is.
// TODO: Do something cryptographic.
func (r *remote) Authenticate(userName upspin.UserName) (int, error) {
	req := &proto.AuthenticateRequest{
		UserName: userName,
	}
	var resp proto.AuthenticateResponse
	err := r.rpcClient.Call("Server.Authenticate", &req, &resp)
	return resp.ID, err
}

// ServerUserName implements upspin.Service.
func (r *remote) ServerUserName() string {
	return "" // No one is authenticated.
}

// Dial always returns the same instance, so there is only one instance of the service
// running in the address space. It ignores the address within the endpoint but
// requires that the transport be InProcess.
func (*remote) Dial(context *upspin.Context, endpoint upspin.Endpoint) (upspin.Service, error) {
	if endpoint.Transport != upspin.Remote {
		return nil, errors.New("remote: unrecognized transport")
	}

	// If we already have an authenticated dial for the endpoint and user
	// return it.
	connections.Lock()
	for _, r := range connections.c {
		if r.endpoint.NetAddr == endpoint.NetAddr && r.userName == context.UserName {
			connections.Unlock()
			return r, nil
		}
	}
	connections.Unlock()

	r := &remote{
		endpoint: endpoint,
		userName: context.UserName,
	}

	var err error
	addr := string(endpoint.NetAddr)
	switch {
	case strings.HasPrefix(addr, "http://"):
		r.rpcClient, err = rpc.DialHTTP("tcp", addr[7:])
	default:
		err = fmt.Errorf("unrecognized net address in remote: %q", addr)
	}
	if err != nil {
		return nil, err
	}
	r.id, err = r.Authenticate(context.UserName)
	if err != nil {
		return nil, err
	}

	connections.Lock()
	connections.c = append(connections.c, r)
	connections.Unlock()
	return r, nil
}

// Endpoint implements upspin.Directory.Endpoint.
func (r *remote) Endpoint() upspin.Endpoint {
	return r.endpoint
}

const transport = upspin.Remote

func init() {
	r := &remote{} // uninitialized until Dial time.
	bind.RegisterDirectory(transport, r)
}