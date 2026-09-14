# terraform-aws-linux-instance

Creates one private Ubuntu Linux EC2 instance for service ranges. The module
uses SSM rather than inbound SSH, requires IMDSv2, encrypts all EBS volumes,
and allows traffic only from inside the supplied VPC.
