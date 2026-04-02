# Grab the list of availability zones
data "aws_availability_zones" "available" {
  state = "available"
}

data "aws_region" "current" {}

# Get all route tables associated with private subnets
data "aws_route_tables" "private" {
  vpc_id = aws_vpc.main.id
  filter {
    name   = "association.subnet-id"
    values = [for subnet in aws_subnet.private : subnet.id]
  }
}
