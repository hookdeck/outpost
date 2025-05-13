# ========== PostgreSQL Resources ==========

# Security Group for RDS
resource "aws_security_group" "rds_sg" {
  name        = "outpost-loadtest-rds-sg"
  description = "Allow traffic to RDS from EKS"
  vpc_id      = aws_vpc.main.id

  ingress {
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [aws_eks_cluster.main.vpc_config[0].cluster_security_group_id]
  }

  tags = {
    Name = "outpost-loadtest-rds-sg"
  }
}

# Subnet Group for RDS
resource "aws_db_subnet_group" "rds" {
  name       = "outpost-loadtest-rds-subnet-group"
  subnet_ids = aws_subnet.private[*].id

  tags = {
    Name = "outpost-loadtest-rds-subnet-group"
  }
}

# RDS PostgreSQL Instance
resource "aws_db_instance" "postgres" {
  allocated_storage       = 20
  storage_type            = "gp2"
  engine                  = "postgres"
  engine_version          = "14.17"
  instance_class          = "db.t3.micro"
  identifier              = "outpost-postgres"
  db_name                 = "outpost"
  username                = "postgres"
  password                = "temppassword"
  skip_final_snapshot     = true
  multi_az                = false
  publicly_accessible     = false
  vpc_security_group_ids  = [aws_security_group.rds_sg.id]
  db_subnet_group_name    = aws_db_subnet_group.rds.name
  backup_retention_period = 0

  tags = {
    Name = "outpost-loadtest-postgres"
  }
}

# ========== ElastiCache Redis Resources ==========

# Security Group for ElastiCache
resource "aws_security_group" "elasticache_sg" {
  name        = "outpost-loadtest-elasticache-sg"
  description = "Allow traffic to ElastiCache from EKS"
  vpc_id      = aws_vpc.main.id

  ingress {
    from_port       = 6379
    to_port         = 6379
    protocol        = "tcp"
    security_groups = [aws_eks_cluster.main.vpc_config[0].cluster_security_group_id]
  }

  tags = {
    Name = "outpost-loadtest-elasticache-sg"
  }
}

# Subnet Group for ElastiCache
resource "aws_elasticache_subnet_group" "elasticache" {
  name       = "outpost-loadtest-elasticache-subnet-group"
  subnet_ids = aws_subnet.private[*].id
}

# ElastiCache Redis
resource "aws_elasticache_cluster" "redis" {
  cluster_id           = "outpost-redis"
  engine               = "redis"
  node_type            = "cache.t3.micro"
  num_cache_nodes      = 1
  parameter_group_name = "default.redis6.x"
  engine_version       = "6.2"
  port                 = 6379
  subnet_group_name    = aws_elasticache_subnet_group.elasticache.name
  security_group_ids   = [aws_security_group.elasticache_sg.id]

  tags = {
    Name = "outpost-loadtest-redis"
  }
}
