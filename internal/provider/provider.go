package provider

import "context"

type PodRequest struct {
	Name           string
	Image          string
	GPUs           int
	GPUIDs         []string
	DiskGB         int
	TerminateAfter int64
	SSHKey         string
	PublicSSH      bool
}
type Pod struct {
	ID        string
	Name      string
	Status    string
	HourlyUSD float64
	SSHHost   string
	SSHPort   int
}
type SSHInfo struct {
	Host       string
	Port       int
	User       string
	PrivateKey string
	KnownHosts string
}
type RunpodAPI interface {
	CreatePod(context.Context, PodRequest) (Pod, error)
	GetPod(context.Context, string) (Pod, error)
	ListPods(context.Context, string) ([]Pod, error)
	DeletePod(context.Context, string) error
}
type SSHProvider interface {
	SSHInfo(context.Context, string) (SSHInfo, error)
}
type Mock struct{ Pods map[string]Pod }

func (m *Mock) CreatePod(_ context.Context, r PodRequest) (Pod, error) {
	p := Pod{ID: r.Name, Name: r.Name, Status: "RUNNING"}
	if m.Pods == nil {
		m.Pods = map[string]Pod{}
	}
	m.Pods[p.ID] = p
	return p, nil
}
func (m *Mock) GetPod(_ context.Context, id string) (Pod, error) {
	p, ok := m.Pods[id]
	if !ok {
		return Pod{}, context.Canceled
	}
	return p, nil
}
func (m *Mock) ListPods(_ context.Context, name string) ([]Pod, error) {
	var o []Pod
	for _, p := range m.Pods {
		if p.Name == name {
			o = append(o, p)
		}
	}
	return o, nil
}
func (m *Mock) DeletePod(_ context.Context, id string) error { delete(m.Pods, id); return nil }
