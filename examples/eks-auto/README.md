# EKS Auto Mode

This project defines an EKS Auto Mode cluster with the built-in `system` and
`general-purpose` node pools, plus a Fargate profile. Pods in the `default` namespace with the
label `compute=fargate` use Fargate; other compatible workloads can use the Auto Mode node pools.

The example installs the CoreDNS EKS add-on because the cluster combines Auto Mode and Fargate.
AWS documents CoreDNS as compatible with both compute types. Auto Mode provides the other cluster
networking, load balancing, and block storage capabilities used by this example.

Update `private-subnet-ids` in `dev.ub` with private subnets from your VPC before applying. The
private subnets need outbound access for Auto Mode nodes and Fargate pods through NAT or the
required VPC endpoints. Fargate pods cannot use subnets with a direct route to an internet gateway.
The cluster API endpoint is public and private so local `unobin` commands can reach it without
running from inside the VPC.

If you set `kubernetes-version`, use a version supported by EKS Auto Mode. EKS clusters, Auto Mode
EC2 instances, Fargate pods, NAT gateways, and related data transfer can incur meaningful AWS
charges. Destroy the stack when you are done.

```sh
unobin compile -o ./build --build
./build/eks-auto pin -c dev.ub
./build/eks-auto plan -c dev.ub -o plan.json
./build/eks-auto apply plan.json
./build/eks-auto plan -c dev.ub --destroy -o destroy-plan.json
./build/eks-auto apply destroy-plan.json
```
