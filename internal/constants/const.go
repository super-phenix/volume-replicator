package constants

const (
	LockName                               = "spx-volume-replicator-leader-election"
	VrcValueAnnotation                     = "replication.superphenix.net/class"
	VrcSelectorAnnotation                  = "replication.superphenix.net/classSelector"
	PauseAnnotation                        = "replication.superphenix.net/pause"
	ParentLabel                            = "replication.superphenix.net/parent"
	StorageClassGroup                      = "replication.superphenix.net/storageClassGroup"
	ReplicationStateAnnotation             = "replication.superphenix.net/replicationState"
	StorageProvisionerAnnotation           = "volume.kubernetes.io/storage-provisioner"
	DeprecatedStorageProvisionerAnnotation = "volume.beta.kubernetes.io/storage-provisioner"
)
