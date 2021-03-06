package stub

import (
	"context"
	"errors"
	"testing"

	"github.com/Percona-Lab/percona-server-mongodb-operator/internal/sdk/mocks"
	"github.com/Percona-Lab/percona-server-mongodb-operator/pkg/apis/psmdb/v1alpha1"

	"github.com/operator-framework/operator-sdk/pkg/sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHandlerHandle(t *testing.T) {
	psmdb := &v1alpha1.PerconaServerMongoDB{
		ObjectMeta: metav1.ObjectMeta{
			Name:      t.Name(),
			Namespace: "test",
		},
		Spec: v1alpha1.PerconaServerMongoDBSpec{
			Secrets: &v1alpha1.SecretsSpec{
				Key:   defaultKeySecretName,
				Users: defaultUsersSecretName,
			},
			Replsets: []*v1alpha1.ReplsetSpec{
				{
					Name: defaultReplsetName,
					Size: defaultMongodSize,
					ResourcesSpec: &v1alpha1.ResourcesSpec{
						Limits: &v1alpha1.ResourceSpecRequirements{
							Cpu:     "1",
							Memory:  "1G",
							Storage: "1G",
						},
						Requests: &v1alpha1.ResourceSpecRequirements{
							Cpu:    "1",
							Memory: "1G",
						},
					},
				},
			},
			Mongod: &v1alpha1.MongodSpec{
				Net: &v1alpha1.MongodSpecNet{
					Port: 99999,
				},
			},
		},
	}
	event := sdk.Event{
		Object: psmdb,
	}

	client := &mocks.Client{}
	client.On("Create", mock.AnythingOfType("*v1.Secret")).Return(nil)
	client.On("Create", mock.AnythingOfType("*v1.Service")).Return(nil)
	client.On("Create", mock.AnythingOfType("*v1.StatefulSet")).Return(nil)
	client.On("Get", mock.AnythingOfType("*v1.Secret")).Return(nil)
	client.On("Get", mock.AnythingOfType("*v1.StatefulSet")).Return(errors.New("not found error")).Once()
	client.On("List",
		"test",
		mock.AnythingOfType("*v1.PodList"),
		mock.AnythingOfType("sdk.ListOption"),
	).Return(nil)

	h := &Handler{
		client: client,
		serverVersion: &v1alpha1.ServerVersion{
			Platform: v1alpha1.PlatformKubernetes,
		},
		watchdogQuit: make(chan bool),
	}

	// test Handler with no existing stateful sets
	assert.NoError(t, h.Handle(context.TODO(), event))
	client.AssertExpectations(t)

	// test Handler with existing stateful set (mocked)
	client.On("Get", mock.AnythingOfType("*v1.StatefulSet")).Return(nil).Run(func(args mock.Arguments) {
		set := args.Get(0).(*appsv1.StatefulSet)
		set.Spec = appsv1.StatefulSetSpec{
			Replicas: &defaultMongodSize,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: mongodContainerName,
						},
					},
				},
			},
		}
	})
	client.On("Update", mock.AnythingOfType("*v1.StatefulSet")).Return(nil)
	assert.NoError(t, h.Handle(context.TODO(), event))
	client.AssertExpectations(t)

	// check last call was a Create with a corev1.Service object:
	calls := len(client.Calls)
	lastCall := client.Calls[calls-1]
	assert.Equal(t, "Create", lastCall.Method)
	assert.IsType(t, &corev1.Service{}, lastCall.Arguments.Get(0))

	// test watchdog is started when 1+ replsets are initializaed
	client.On("Update", mock.AnythingOfType("*v1alpha1.PerconaServerMongoDB")).Return(nil)
	assert.Nil(t, h.watchdog)
	psmdb.Status = v1alpha1.PerconaServerMongoDBStatus{
		Replsets: []*v1alpha1.ReplsetStatus{
			{
				Name:        defaultReplsetName,
				Initialized: true,
			},
		},
	}
	assert.NoError(t, h.Handle(context.TODO(), event))
	assert.NotNil(t, h.watchdog)

	// test watchdog is stopped by a 'Deleted' SDK event
	event.Deleted = true
	assert.NoError(t, h.Handle(context.TODO(), event))
	assert.Nil(t, h.watchdog)
}
