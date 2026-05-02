# Deploy Crucible IAP on AWS (ECS + EC2)

This guide walks through deploying Crucible IAP on AWS using RDS PostgreSQL, S3, ECS Fargate for the API, and an EC2 instance for the worker. It replaces the default Docker Compose stack with managed AWS services.

## Contents

1. [Overview](#overview)
2. [Prerequisites](#prerequisites)
3. [RDS PostgreSQL](#rds-postgresql)
4. [S3 buckets](#s3-buckets)
5. [Secrets Manager](#secrets-manager)
6. [ECS Fargate — API](#ecs-fargate--api)
7. [EC2 instance — worker](#ec2-instance--worker)
8. [ALB and ACM](#alb-and-acm)
9. [Verification](#verification)
10. [Scaling notes](#scaling-notes)

---

## Overview

Crucible has two processes with different hosting requirements:

| Component | Where | Why |
| --- | --- | --- |
| `crucible-api` | ECS Fargate | Stateless HTTP server; no Docker socket needed; scales horizontally |
| `crucible-worker` | EC2 instance | Spawns Docker containers to run Terraform/Ansible/Pulumi; requires Docker socket |
| PostgreSQL | RDS | Managed, Multi-AZ, automated backups |
| Artifact/log/registry storage | S3 | Replaces MinIO; the MinIO client is S3-compatible — set `MINIO_ENDPOINT` to the S3 regional endpoint |
| HTTPS termination | ALB + ACM | Terminates TLS, forwards port 443 → ECS service on 8080 |
| Secrets | Secrets Manager | `SECRET_KEY`, `DATABASE_URL`, S3 credentials injected into ECS task definition |

**Key constraint:** Fargate does not expose the Docker socket to containers. The worker must run on an EC2 instance where it can bind-mount `/var/run/docker.sock`. The API has no such dependency and runs cleanly on Fargate.

```
Internet
   │
   ▼
ALB (HTTPS/443)
   │
   ▼
ECS Fargate (crucible-api :8080)
   │  ▲
   │  └── HTTP callbacks from worker
   ▼
RDS PostgreSQL
   ▲
   │
EC2 instance (crucible-worker)
   ├── Docker socket (run spawn)
   └── S3 / Secrets Manager (same IAM policy as API)
```

---

## Prerequisites

**Tools**

- AWS CLI v2 configured with credentials (`aws configure` or instance profile)
- An existing VPC with at least two private subnets (for RDS Multi-AZ and ECS) and two public subnets (for the ALB)
- A domain name with DNS you can update (for ACM certificate validation)

**IAM permissions required to complete this guide**

| Service | Actions |
| --- | --- |
| RDS | `rds:CreateDBInstance`, `rds:CreateDBSubnetGroup`, `rds:ModifyDBInstance` |
| S3 | `s3:CreateBucket`, `s3:PutBucketPolicy`, `s3:PutPublicAccessBlock` |
| Secrets Manager | `secretsmanager:CreateSecret`, `secretsmanager:PutSecretValue` |
| ECS | `ecs:CreateCluster`, `ecs:RegisterTaskDefinition`, `ecs:CreateService` |
| EC2 | `ec2:RunInstances`, `ec2:CreateSecurityGroup`, `ec2:AuthorizeSecurityGroupIngress` |
| IAM | `iam:CreateRole`, `iam:AttachRolePolicy`, `iam:PassRole` |
| ACM | `acm:RequestCertificate`, `acm:DescribeCertificate` |
| ELB | `elasticloadbalancing:CreateLoadBalancer`, `elasticloadbalancing:CreateTargetGroup` |

**Naming convention used in this guide**

Replace the following placeholders throughout:

| Placeholder | Example |
| --- | --- |
| `<region>` | `us-east-1` |
| `<account-id>` | `123456789012` |
| `<vpc-id>` | `vpc-0abc123` |
| `<private-subnet-a>`, `<private-subnet-b>` | `subnet-0aaa`, `subnet-0bbb` |
| `<public-subnet-a>`, `<public-subnet-b>` | `subnet-0ccc`, `subnet-0ddd` |
| `crucible.example.com` | Your actual domain |

---

## RDS PostgreSQL

### Security group

```bash
aws ec2 create-security-group \
  --group-name crucible-rds-sg \
  --description "Crucible RDS access" \
  --vpc-id <vpc-id>

# Allow inbound 5432 from the ECS and worker security groups only
# (add these rules after creating those groups — see the ECS and EC2 sections)
```

### Parameter group

```bash
aws rds create-db-parameter-group \
  --db-parameter-group-name crucible-pg16 \
  --db-parameter-group-family postgres16 \
  --description "Crucible IAP parameter group"

aws rds modify-db-parameter-group \
  --db-parameter-group-name crucible-pg16 \
  --parameters "ParameterName=max_connections,ParameterValue=200,ApplyMethod=pending-reboot"
```

### Subnet group

```bash
aws rds create-db-subnet-group \
  --db-subnet-group-name crucible-subnet-group \
  --db-subnet-group-description "Crucible IAP subnets" \
  --subnet-ids <private-subnet-a> <private-subnet-b>
```

### Create the instance

**Development (single-AZ, cheapest):**

```bash
aws rds create-db-instance \
  --db-instance-identifier crucible-db \
  --db-instance-class db.t3.micro \
  --engine postgres \
  --engine-version "16.3" \
  --master-username crucible \
  --master-user-password "<strong-password>" \
  --db-name crucible \
  --db-subnet-group-name crucible-subnet-group \
  --vpc-security-group-ids <rds-sg-id> \
  --db-parameter-group-name crucible-pg16 \
  --storage-type gp3 \
  --allocated-storage 20 \
  --no-multi-az \
  --no-publicly-accessible
```

**Production (Multi-AZ):** Add `--multi-az` and increase to `--db-instance-class db.t3.small`.

Wait for the instance to become available, then note the endpoint:

```bash
aws rds describe-db-instances \
  --db-instance-identifier crucible-db \
  --query "DBInstances[0].Endpoint.Address" \
  --output text
```

---

## S3 buckets

Crucible uses three buckets. The MinIO client in the storage layer is S3-compatible — point `MINIO_ENDPOINT` at the S3 regional endpoint and it works without any code changes.

### Create the buckets

```bash
for BUCKET in crucible-runs crucible-state crucible-registry; do
  aws s3api create-bucket \
    --bucket "${BUCKET}" \
    --region <region> \
    --create-bucket-configuration LocationConstraint=<region>

  aws s3api put-public-access-block \
    --bucket "${BUCKET}" \
    --public-access-block-configuration \
      "BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true"
done
```

> **Note:** For `us-east-1`, omit `--create-bucket-configuration` — that region does not accept a `LocationConstraint`.

Versioning is left off. These buckets hold run artifacts and logs — versioning adds cost without a useful recovery story (Crucible tracks run history in the database, not via S3 object versions).

### IAM policy for Crucible access

Create a least-privilege policy. Save the following as `crucible-s3-policy.json`:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "CrucibleS3Access",
      "Effect": "Allow",
      "Action": [
        "s3:PutObject",
        "s3:GetObject",
        "s3:DeleteObject",
        "s3:ListBucket"
      ],
      "Resource": [
        "arn:aws:s3:::crucible-runs",
        "arn:aws:s3:::crucible-runs/*",
        "arn:aws:s3:::crucible-state",
        "arn:aws:s3:::crucible-state/*",
        "arn:aws:s3:::crucible-registry",
        "arn:aws:s3:::crucible-registry/*"
      ]
    }
  ]
}
```

```bash
aws iam create-policy \
  --policy-name CrucibleS3Access \
  --policy-document file://crucible-s3-policy.json
```

### IAM user (static keys)

If you are not using instance profiles (the EC2 worker makes instance profiles straightforward for the worker, but the ECS task also needs access):

```bash
aws iam create-user --user-name crucible-s3

aws iam attach-user-policy \
  --user-name crucible-s3 \
  --policy-arn arn:aws:iam::<account-id>:policy/CrucibleS3Access

aws iam create-access-key --user-name crucible-s3
```

Save the `AccessKeyId` and `SecretAccessKey` — you will store them in Secrets Manager next.

---

## Secrets Manager

Store all sensitive config in Secrets Manager and reference them from the ECS task definition. This avoids hardcoding secrets in task definitions or environment variables.

### Store secrets

```bash
# Database URL
aws secretsmanager create-secret \
  --name crucible/database-url \
  --secret-string "postgres://crucible:<password>@<rds-endpoint>:5432/crucible?sslmode=require"

# Application secret key (generate 64 random chars)
aws secretsmanager create-secret \
  --name crucible/secret-key \
  --secret-string "$(openssl rand -hex 32)"

# S3 credentials
aws secretsmanager create-secret \
  --name crucible/s3-access-key \
  --secret-string "<iam-access-key-id>"

aws secretsmanager create-secret \
  --name crucible/s3-secret-key \
  --secret-string "<iam-secret-access-key>"
```

### ECS task execution role policy

The ECS task execution role needs permission to fetch these secrets at task startup. Add this inline policy to your `ecsTaskExecutionRole`:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "secretsmanager:GetSecretValue"
      ],
      "Resource": [
        "arn:aws:secretsmanager:<region>:<account-id>:secret:crucible/*"
      ]
    }
  ]
}
```

```bash
aws iam put-role-policy \
  --role-name ecsTaskExecutionRole \
  --policy-name CrucibleSecretsAccess \
  --policy-document file://crucible-secrets-policy.json
```

---

## ECS Fargate — API

### Create the cluster

```bash
aws ecs create-cluster --cluster-name crucible
```

### Security group

```bash
aws ec2 create-security-group \
  --group-name crucible-ecs-sg \
  --description "Crucible ECS tasks" \
  --vpc-id <vpc-id>

# Allow inbound 8080 from the ALB security group
aws ec2 authorize-security-group-ingress \
  --group-id <ecs-sg-id> \
  --protocol tcp \
  --port 8080 \
  --source-group <alb-sg-id>

# Allow outbound to RDS
aws ec2 authorize-security-group-ingress \
  --group-id <rds-sg-id> \
  --protocol tcp \
  --port 5432 \
  --source-group <ecs-sg-id>
```

### Task definition

Save the following as `crucible-api-task.json`. The `secrets` block pulls values from Secrets Manager at task startup — the task definition itself contains only ARNs, never plaintext.

```json
{
  "family": "crucible-api",
  "networkMode": "awsvpc",
  "requiresCompatibilities": ["FARGATE"],
  "cpu": "512",
  "memory": "1024",
  "executionRoleArn": "arn:aws:iam::<account-id>:role/ecsTaskExecutionRole",
  "containerDefinitions": [
    {
      "name": "crucible-api",
      "image": "ghcr.io/ponack/crucible-iap:latest",
      "portMappings": [
        {
          "containerPort": 8080,
          "protocol": "tcp"
        }
      ],
      "environment": [
        { "name": "CRUCIBLE_ENV",       "value": "production" },
        { "name": "CRUCIBLE_BASE_URL",  "value": "https://crucible.example.com" },
        { "name": "MINIO_ENDPOINT",     "value": "s3.<region>.amazonaws.com" },
        { "name": "MINIO_USE_SSL",      "value": "true" },
        { "name": "MINIO_BUCKET_RUNS",     "value": "crucible-runs" },
        { "name": "MINIO_BUCKET_STATE",    "value": "crucible-state" },
        { "name": "MINIO_BUCKET_REGISTRY", "value": "crucible-registry" }
      ],
      "secrets": [
        {
          "name": "DATABASE_URL",
          "valueFrom": "arn:aws:secretsmanager:<region>:<account-id>:secret:crucible/database-url"
        },
        {
          "name": "SECRET_KEY",
          "valueFrom": "arn:aws:secretsmanager:<region>:<account-id>:secret:crucible/secret-key"
        },
        {
          "name": "MINIO_ACCESS_KEY",
          "valueFrom": "arn:aws:secretsmanager:<region>:<account-id>:secret:crucible/s3-access-key"
        },
        {
          "name": "MINIO_SECRET_KEY",
          "valueFrom": "arn:aws:secretsmanager:<region>:<account-id>:secret:crucible/s3-secret-key"
        }
      ],
      "healthCheck": {
        "command": ["CMD-SHELL", "curl -f http://localhost:8080/health || exit 1"],
        "interval": 30,
        "timeout": 5,
        "retries": 3,
        "startPeriod": 10
      },
      "logConfiguration": {
        "logDriver": "awslogs",
        "options": {
          "awslogs-group": "/ecs/crucible-api",
          "awslogs-region": "<region>",
          "awslogs-stream-prefix": "ecs"
        }
      }
    }
  ]
}
```

```bash
# Create the log group
aws logs create-log-group --log-group-name /ecs/crucible-api

# Register the task definition
aws ecs register-task-definition --cli-input-json file://crucible-api-task.json
```

### ECS service

```bash
aws ecs create-service \
  --cluster crucible \
  --service-name crucible-api \
  --task-definition crucible-api \
  --desired-count 2 \
  --launch-type FARGATE \
  --network-configuration "awsvpcConfiguration={subnets=[<private-subnet-a>,<private-subnet-b>],securityGroups=[<ecs-sg-id>],assignPublicIp=DISABLED}" \
  --load-balancers "targetGroupArn=<target-group-arn>,containerName=crucible-api,containerPort=8080" \
  --health-check-grace-period-seconds 60
```

> Create the ALB and target group first (see [ALB and ACM](#alb-and-acm)), then come back to create the service with the `--load-balancers` argument.

---

## EC2 instance — worker

The worker spawns Docker containers to execute IaC runs. It needs access to the Docker socket, which Fargate does not provide. A single EC2 instance is sufficient to start; add more for parallel run capacity (see [Scaling notes](#scaling-notes)).

### Security group

```bash
aws ec2 create-security-group \
  --group-name crucible-worker-sg \
  --description "Crucible worker — no inbound needed" \
  --vpc-id <vpc-id>

# No inbound rules needed — the worker polls for jobs, it does not receive inbound connections

# Allow RDS access from the worker
aws ec2 authorize-security-group-ingress \
  --group-id <rds-sg-id> \
  --protocol tcp \
  --port 5432 \
  --source-group <worker-sg-id>
```

The worker needs outbound to: RDS on port 5432, S3 (HTTPS 443), Secrets Manager (HTTPS 443), and the Crucible API URL for run callbacks. All of these are outbound only — no inbound rules are required on the worker's security group.

### Launch the instance

```bash
aws ec2 run-instances \
  --image-id resolve:ssm:/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-x86_64 \
  --instance-type t3.small \
  --subnet-id <private-subnet-a> \
  --security-group-ids <worker-sg-id> \
  --iam-instance-profile Name=crucible-worker-profile \
  --tag-specifications 'ResourceType=instance,Tags=[{Key=Name,Value=crucible-worker}]' \
  --user-data file://worker-userdata.sh
```

**Instance profile** — attach an IAM role with the `CrucibleS3Access` policy and Secrets Manager read access so the worker can authenticate to S3 and fetch its own secrets without static keys.

### User data script

Save the following as `worker-userdata.sh`:

```bash
#!/bin/bash
set -euo pipefail

# Install Docker
dnf install -y docker
systemctl enable --now docker

# Pull the Crucible image
docker pull ghcr.io/ponack/crucible-iap:latest

# Write the systemd unit
cat > /etc/systemd/system/crucible-worker.service <<'EOF'
[Unit]
Description=Crucible IAP Worker
After=docker.service
Requires=docker.service

[Service]
Restart=always
RestartSec=5
ExecStartPre=-/usr/bin/docker stop crucible-worker
ExecStartPre=-/usr/bin/docker rm crucible-worker
ExecStart=/usr/bin/docker run --rm \
  --name crucible-worker \
  --entrypoint crucible-worker \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e CRUCIBLE_ENV=production \
  -e CRUCIBLE_BASE_URL=https://crucible.example.com \
  -e RUNNER_API_URL=https://crucible.example.com \
  -e MINIO_ENDPOINT=s3.<region>.amazonaws.com \
  -e MINIO_USE_SSL=true \
  -e MINIO_BUCKET_RUNS=crucible-runs \
  -e MINIO_BUCKET_STATE=crucible-state \
  -e MINIO_BUCKET_REGISTRY=crucible-registry \
  -e DATABASE_URL=__fetched_below__ \
  -e SECRET_KEY=__fetched_below__ \
  -e MINIO_ACCESS_KEY=__fetched_below__ \
  -e MINIO_SECRET_KEY=__fetched_below__ \
  ghcr.io/ponack/crucible-iap:latest
ExecStop=/usr/bin/docker stop crucible-worker

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
```

**Fetching secrets on the instance** — the unit file above uses placeholder values for secrets. In practice, use a small wrapper script that fetches values from Secrets Manager at start time and passes them as `-e` flags, or use the AWS SSM Parameter Store `EnvironmentFile` pattern:

```bash
# /usr/local/bin/crucible-worker-env.sh — runs before the container starts
#!/bin/bash
aws secretsmanager get-secret-value \
  --secret-id crucible/database-url \
  --query SecretString --output text > /run/crucible/DATABASE_URL

aws secretsmanager get-secret-value \
  --secret-id crucible/secret-key \
  --query SecretString --output text > /run/crucible/SECRET_KEY

aws secretsmanager get-secret-value \
  --secret-id crucible/s3-access-key \
  --query SecretString --output text > /run/crucible/MINIO_ACCESS_KEY

aws secretsmanager get-secret-value \
  --secret-id crucible/s3-secret-key \
  --query SecretString --output text > /run/crucible/MINIO_SECRET_KEY
```

Then update the `ExecStart` to source those files and pass them as `--env-file`. Files under `/run/crucible/` are in-memory (tmpfs) and cleared on reboot.

### Verify the worker is running

```bash
# SSH to the instance (via Session Manager — no inbound SSH needed)
aws ssm start-session --target <instance-id>

# On the instance
sudo systemctl status crucible-worker
sudo docker logs crucible-worker --tail 50
```

---

## ALB and ACM

### ACM certificate

Request a certificate for your domain:

```bash
aws acm request-certificate \
  --domain-name crucible.example.com \
  --validation-method DNS \
  --region <region>
```

Complete DNS validation by adding the CNAME record shown in the ACM console. Wait until the certificate status is `ISSUED` before proceeding.

```bash
aws acm describe-certificate \
  --certificate-arn <cert-arn> \
  --query "Certificate.Status"
```

### ALB security group

```bash
aws ec2 create-security-group \
  --group-name crucible-alb-sg \
  --description "Crucible ALB" \
  --vpc-id <vpc-id>

aws ec2 authorize-security-group-ingress \
  --group-id <alb-sg-id> \
  --protocol tcp --port 443 --cidr 0.0.0.0/0

aws ec2 authorize-security-group-ingress \
  --group-id <alb-sg-id> \
  --protocol tcp --port 80 --cidr 0.0.0.0/0
```

### Create the ALB

```bash
aws elbv2 create-load-balancer \
  --name crucible-alb \
  --subnets <public-subnet-a> <public-subnet-b> \
  --security-groups <alb-sg-id> \
  --scheme internet-facing \
  --type application
```

### Target group

```bash
aws elbv2 create-target-group \
  --name crucible-api-tg \
  --protocol HTTP \
  --port 8080 \
  --vpc-id <vpc-id> \
  --target-type ip \
  --health-check-protocol HTTP \
  --health-check-path /health \
  --health-check-interval-seconds 30 \
  --healthy-threshold-count 2 \
  --unhealthy-threshold-count 3
```

### Listeners

```bash
# HTTPS listener — forward to ECS
aws elbv2 create-listener \
  --load-balancer-arn <alb-arn> \
  --protocol HTTPS \
  --port 443 \
  --certificates CertificateArn=<cert-arn> \
  --default-actions Type=forward,TargetGroupArn=<target-group-arn>

# HTTP listener — redirect to HTTPS
aws elbv2 create-listener \
  --load-balancer-arn <alb-arn> \
  --protocol HTTP \
  --port 80 \
  --default-actions '[{"Type":"redirect","RedirectConfig":{"Protocol":"HTTPS","Port":"443","StatusCode":"HTTP_301"}}]'
```

### DNS

Point your domain at the ALB DNS name:

```bash
aws elbv2 describe-load-balancers \
  --names crucible-alb \
  --query "LoadBalancers[0].DNSName" \
  --output text
```

Create a CNAME (or alias A record if using Route 53) pointing `crucible.example.com` to the ALB DNS name.

---

## Verification

### Health check

```bash
curl -s https://crucible.example.com/health
```

Expected response:

```json
{"status":"ok"}
```

### Check ECS task logs

```bash
# List running tasks
aws ecs list-tasks --cluster crucible --service-name crucible-api

# Fetch logs (use the log stream from the task ID)
aws logs get-log-events \
  --log-group-name /ecs/crucible-api \
  --log-stream-name ecs/crucible-api/<task-id> \
  --limit 50
```

### Check worker logs

```bash
aws ssm start-session --target <worker-instance-id>
# then on the instance:
sudo docker logs crucible-worker --tail 100 --follow
```

### Trigger a test run

1. Open `https://crucible.example.com` in your browser and log in
2. Create a stack pointing at a simple Terraform module (e.g. a hello-world that outputs a local value)
3. Trigger a run manually from the stack detail page
4. Confirm the run completes and the log appears in the Runs view — this validates the full path: API → database → worker → Docker run → artifact upload to S3

### Smoke-test S3 access

```bash
aws s3 ls s3://crucible-runs/
aws s3 ls s3://crucible-state/
aws s3 ls s3://crucible-registry/
```

After a completed run, you should see objects under `crucible-runs/`.

---

## Scaling notes

### ECS API auto-scaling

Add a target-tracking scaling policy on the ECS service to scale API replicas on CPU utilization:

```bash
aws application-autoscaling register-scalable-target \
  --service-namespace ecs \
  --resource-id service/crucible/crucible-api \
  --scalable-dimension ecs:service:DesiredCount \
  --min-capacity 2 \
  --max-capacity 10

aws application-autoscaling put-scaling-policy \
  --service-namespace ecs \
  --resource-id service/crucible/crucible-api \
  --scalable-dimension ecs:service:DesiredCount \
  --policy-name crucible-api-cpu \
  --policy-type TargetTrackingScaling \
  --target-tracking-scaling-policy-configuration '{
    "TargetValue": 60.0,
    "PredefinedMetricSpecification": {
      "PredefinedMetricType": "ECSServiceAverageCPUUtilization"
    },
    "ScaleInCooldown": 120,
    "ScaleOutCooldown": 60
  }'
```

The API is stateless — additional replicas are safe at any time.

### Multiple worker nodes

Each additional EC2 worker instance registers independently with the River job queue and picks up run jobs. To add capacity, launch additional instances with the same user data and security group. There is no coordination required — River handles job distribution.

For burst capacity, consider using an Auto Scaling group for the worker fleet. Set a scheduled scale-out during business hours and scale-in overnight, or use a custom CloudWatch metric based on pending run count.

### RDS connection pooling

With multiple API replicas and worker nodes, connection count to RDS can grow quickly. Two options:

| Approach | When to use |
| --- | --- |
| Increase `max_connections` in the parameter group | Adequate for small fleets (< 20 total replicas + workers) |
| Add PgBouncer as a sidecar or standalone service | Recommended when API replicas exceed ~10 or when you see `too many connections` errors |

For PgBouncer, run it as a second container in the ECS task definition or as a separate ECS service. Set it to `transaction` mode (compatible with Crucible's query patterns) and point `DATABASE_URL` at the PgBouncer endpoint instead of RDS directly. Use the RDS instance endpoint only for the PgBouncer → RDS connection.

> **Note:** RDS Proxy is an alternative to PgBouncer that requires no additional infrastructure. It adds latency compared to PgBouncer in transaction mode but integrates natively with IAM authentication and Secrets Manager rotation.
