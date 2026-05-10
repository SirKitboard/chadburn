package core

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

// OfficialDockerClient implements DockerClient using the official Docker client
type OfficialDockerClient struct {
	client *client.Client
	ctx    context.Context
}

// NewDockerClient creates a new Docker client using the official Docker client
func NewDockerClient() (DockerClient, error) {
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}

	return &OfficialDockerClient{
		client: cli,
		ctx:    ctx,
	}, nil
}

// CreateExecJob creates a new OfficialExecJob
func (c *OfficialDockerClient) CreateExecJob() *OfficialExecJob {
	return NewOfficialExecJob(c)
}

// CreateRunJob creates a new OfficialRunJob
func (c *OfficialDockerClient) CreateRunJob() *OfficialRunJob {
	return NewOfficialRunJob(c)
}

// CreateServiceJob creates a new OfficialServiceJob
func (c *OfficialDockerClient) CreateServiceJob() *OfficialServiceJob {
	return NewOfficialServiceJob(c)
}

// CreateLifecycleJob creates a new OfficialLifecycleJob
func (c *OfficialDockerClient) CreateLifecycleJob() *OfficialLifecycleJob {
	return NewOfficialLifecycleJob(c)
}

// ListContainers lists containers with the given filters
func (c *OfficialDockerClient) ListContainers(filterMap map[string][]string) ([]Container, error) {
	// Convert filters to Docker filter format
	filterArgs := filters.NewArgs()
	for k, values := range filterMap {
		for _, v := range values {
			filterArgs.Add(k, v)
		}
	}

	// List containers
	containers, err := c.client.ContainerList(c.ctx, container.ListOptions{
		Filters: filterArgs,
	})
	if err != nil {
		return nil, err
	}

	// Convert to our Container type
	result := make([]Container, len(containers))
	for i, container := range containers {
		name := ""
		if len(container.Names) > 0 {
			name = strings.TrimPrefix(container.Names[0], "/")
		}
		result[i] = Container{
			ID:     container.ID,
			Name:   name,
			Labels: container.Labels,
		}
		result[i].Config.Labels = container.Labels
	}

	return result, nil
}

// InspectContainer inspects a container by ID
func (c *OfficialDockerClient) InspectContainer(id string) (*Container, error) {
	containerInfo, err := c.client.ContainerInspect(c.ctx, id)
	if err != nil {
		return nil, err
	}

	result := &Container{
		ID:     containerInfo.ID,
		Name:   strings.TrimPrefix(containerInfo.Name, "/"),
		Labels: containerInfo.Config.Labels,
	}
	result.State.Running = containerInfo.State.Running
	result.Config.Labels = containerInfo.Config.Labels

	return result, nil
}

// CreateContainer creates a new container
func (c *OfficialDockerClient) CreateContainer(config *ContainerConfig) (*Container, error) {
	// If a name is requested, remove any stopped container with that name first
	// so repeated runs don't fail with a name conflict.
	if config.Name != "" {
		containers, err := c.client.ContainerList(c.ctx, container.ListOptions{All: true})
		if err == nil {
			for _, ct := range containers {
				for _, n := range ct.Names {
					if strings.TrimPrefix(n, "/") == config.Name && ct.State != "running" {
						_ = c.client.ContainerRemove(c.ctx, ct.ID, container.RemoveOptions{})
					}
				}
			}
		}
	}

	// Convert our config to Docker's config
	containerConfig := &container.Config{
		Image:        config.Image,
		Cmd:          config.Cmd,
		Env:          config.Env,
		WorkingDir:   config.WorkingDir,
		User:         config.User,
		Tty:          config.Tty,
		AttachStdin:  config.AttachStdin,
		AttachStdout: config.AttachStdout,
		AttachStderr: config.AttachStderr,
		Labels:       config.Labels,
	}

	// Set up host config
	hostConfig := &container.HostConfig{}
	if config.HostConfig != nil {
		hostConfig.Binds = config.HostConfig.Binds
		hostConfig.NetworkMode = container.NetworkMode(config.HostConfig.NetworkMode)
	}

	// Create the container
	resp, err := c.client.ContainerCreate(c.ctx, containerConfig, hostConfig, nil, nil, config.Name)
	if err != nil {
		return nil, err
	}

	// Return the container info
	return &Container{
		ID: resp.ID,
	}, nil
}

// StartContainer starts a container
func (c *OfficialDockerClient) StartContainer(id string) error {
	return c.client.ContainerStart(c.ctx, id, container.StartOptions{})
}

// StopContainer stops a container
func (c *OfficialDockerClient) StopContainer(id string) error {
	return c.client.ContainerStop(c.ctx, id, container.StopOptions{})
}

// RemoveContainer removes a container
func (c *OfficialDockerClient) RemoveContainer(id string) error {
	return c.client.ContainerRemove(c.ctx, id, container.RemoveOptions{
		Force: true,
	})
}

// WaitContainer waits for a container to exit and returns its exit code
func (c *OfficialDockerClient) WaitContainer(id string) (int, error) {
	waitCh, errCh := c.client.ContainerWait(c.ctx, id, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		return -1, err
	case res := <-waitCh:
		return int(res.StatusCode), nil
	}
}

// ReadCloserWrapper wraps a bufio.Reader to implement io.ReadCloser
type ReadCloserWrapper struct {
	*bufio.Reader
	closer io.Closer
}

// Close calls the stored closer function
func (r *ReadCloserWrapper) Close() error {
	if r.closer != nil {
		return r.closer.Close()
	}
	return nil
}

// CreateExec creates an exec instance in a container
func (c *OfficialDockerClient) CreateExec(containerID string, cmd []string, config *ExecConfig) (string, error) {
	execConfig := container.ExecOptions{
		User:         config.User,
		Tty:          config.Tty,
		AttachStdin:  config.AttachStdin,
		AttachStdout: config.AttachStdout,
		AttachStderr: config.AttachStderr,
		Cmd:          cmd,
		WorkingDir:   config.WorkingDir,
	}

	resp, err := c.client.ContainerExecCreate(c.ctx, containerID, execConfig)
	if err != nil {
		return "", err
	}

	return resp.ID, nil
}

// StartExec starts an exec instance
func (c *OfficialDockerClient) StartExec(execID string, attachStdout, attachStderr bool) (io.ReadCloser, error) {
	resp, err := c.client.ContainerExecAttach(c.ctx, execID, container.ExecAttachOptions{
		Tty: false,
	})
	if err != nil {
		return nil, err
	}

	// Wrap the reader with a ReadCloser
	return &ReadCloserWrapper{
		Reader: resp.Reader,
		closer: resp.Conn,
	}, nil
}

// InspectExec inspects an exec instance
func (c *OfficialDockerClient) InspectExec(execID string) (*ExecInspect, error) {
	resp, err := c.client.ContainerExecInspect(c.ctx, execID)
	if err != nil {
		return nil, err
	}

	return &ExecInspect{
		ID:       execID,
		Running:  resp.Running,
		ExitCode: resp.ExitCode,
	}, nil
}

// PullImage pulls an image from a registry, using credentials from
// ~/.docker/config.json when available.
func (c *OfficialDockerClient) PullImage(imageName string) error {
	registryAuth, _ := resolveRegistryAuth(imageName)

	resp, err := c.client.ImagePull(c.ctx, imageName, image.PullOptions{
		RegistryAuth: registryAuth,
	})
	if err != nil {
		return err
	}
	defer resp.Close()

	_, err = io.Copy(io.Discard, resp)
	return err
}

// resolveRegistryAuth reads ~/.docker/config.json and returns a base64-encoded
// JSON auth string suitable for image.PullOptions.RegistryAuth. Returns an
// empty string (no error) if credentials are not found, so public images still
// work without any configuration.
func resolveRegistryAuth(imageName string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile(filepath.Join(home, ".docker", "config.json"))
	if err != nil {
		return "", err
	}

	var cfg struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", err
	}

	host := imageRegistryHost(imageName)
	for key, entry := range cfg.Auths {
		if entry.Auth == "" {
			continue
		}
		if strings.Contains(key, host) || strings.Contains(host, key) {
			decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
			if err != nil {
				continue
			}
			parts := strings.SplitN(string(decoded), ":", 2)
			if len(parts) != 2 {
				continue
			}
			authJSON, err := json.Marshal(map[string]string{
				"username": parts[0],
				"password": parts[1],
			})
			if err != nil {
				continue
			}
			return base64.URLEncoding.EncodeToString(authJSON), nil
		}
	}

	return "", nil
}

// imageRegistryHost extracts the registry hostname from a Docker image name.
// e.g. "ghcr.io/user/image:tag" -> "ghcr.io", "ubuntu:22.04" -> "index.docker.io"
func imageRegistryHost(imageName string) string {
	parts := strings.Split(imageName, "/")
	if len(parts) > 1 && (strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":")) {
		return parts[0]
	}
	return "index.docker.io"
}

// WatchEvents watches Docker events and sends them to the provided channel
func (c *OfficialDockerClient) WatchEvents(ctx context.Context, eventCh chan<- *DockerEvent, errCh chan<- error) {
	// Create a context that can be cancelled
	if ctx == nil {
		ctx = c.ctx
	}

	// Watch Docker events with empty filters
	messages, errs := c.client.Events(ctx, events.ListOptions{})

	// Process events
	go func() {
		for {
			select {
			case err := <-errs:
				if err != nil && err != io.EOF {
					errCh <- err
				}
				if err != nil && err == io.EOF {
					errCh <- io.EOF
					return
				}
			case message := <-messages:
				// Only process container events
				if message.Type == events.ContainerEventType {
					event := &DockerEvent{
						Action: string(message.Action),
						ID:     message.ID,
						Type:   string(message.Type),
						Actor: DockerActor{
							ID:         message.Actor.ID,
							Attributes: message.Actor.Attributes,
						},
						Attributes: message.Actor.Attributes,
					}
					eventCh <- event
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Close closes the Docker client
func (c *OfficialDockerClient) Close() error {
	return c.client.Close()
}

// CreateService creates a new service
func (c *OfficialDockerClient) CreateService(config *ServiceConfig) (string, error) {
	// This is a placeholder implementation
	// The official Docker client doesn't support Swarm services directly
	// We would need to implement this using the Swarm API
	return "", fmt.Errorf("CreateService not implemented")
}

// InspectService inspects a service
func (c *OfficialDockerClient) InspectService(id string) (*Service, error) {
	// This is a placeholder implementation
	// The official Docker client doesn't support Swarm services directly
	// We would need to implement this using the Swarm API
	return nil, fmt.Errorf("InspectService not implemented")
}

// ListTasks lists tasks for a service
func (c *OfficialDockerClient) ListTasks(serviceID string) ([]Task, error) {
	// This is a placeholder implementation
	// The official Docker client doesn't support Swarm services directly
	// We would need to implement this using the Swarm API
	return nil, fmt.Errorf("ListTasks not implemented")
}

// RemoveService removes a service
func (c *OfficialDockerClient) RemoveService(id string) error {
	// This is a placeholder implementation
	// The official Docker client doesn't support Swarm services directly
	// We would need to implement this using the Swarm API
	return fmt.Errorf("RemoveService not implemented")
}
