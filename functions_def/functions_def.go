package functions_def

type FunctionName string

const (
	Clusterize             FunctionName = "clusterize"
	ClusterizeFinalization FunctionName = "clusterize_finalization"
	Deploy                 FunctionName = "deploy"
	Protect                FunctionName = "protect"
	Report                 FunctionName = "report"
	Join                   FunctionName = "join"
	JoinFinalization       FunctionName = "join_finalization"
	JoinNfsFinalization    FunctionName = "join_nfs_finalization"
	SetupNFS               FunctionName = "setup_nfs"
	Fetch                  FunctionName = "fetch"
	Status                 FunctionName = "status"
)

type FunctionDef interface {
	GetFunctionCmdDefinition(name FunctionName) string
}
