# AWS SQS Configuration Instructions

[Amazon Simple Queue Service (SQS)](https://aws.amazon.com/sqs/) is a fully managed message queuing service that enables you to decouple and scale microservices, distributed systems, and serverless applications. SQS eliminates the complexity and overhead associated with managing and operating message-oriented middleware. It provides features such as:

- Standard and FIFO queues
- Configurable message retention
- Message filtering
- Dead-letter queues
- Delay queues

## How to configure AWS SQS as an event destination using the AWS CLI

To follow these steps you will need an AWS account, and the [AWS CLI](https://aws.amazon.com/cli/) installed and authenticated.

1. Create a queue if one doesn't exist (optional)

    ```sh
    aws sqs create-queue --queue-name QUEUENAME --region REGION
    ```

2. Create a policy with necessary permissions

    ```sh
    aws iam create-policy --policy-name POLICYNAME --policy-document '{
      "Version": "2012-10-17",
      "Statement": [
        {
          "Effect": "Allow",
          "Action": [
            "sqs:SendMessage"
          ],
          "Resource": "arn:aws:sqs:REGION:ACCOUNTID:QUEUENAME"
        }
      ]
    }'
    ```

3. Create a user

    ```sh
    aws iam create-user --user-name USERNAME
    ```

4. Attach the policy to the user

    ```sh
    aws iam attach-user-policy --user-name USERNAME --policy-arn arn:aws:iam::ACCOUNTID:policy/POLICYNAME
    ```

5. Create an Access Key

    ```sh
    aws iam create-access-key --user-name USERNAME
    ```

6. Configure your AWS SQS Event Destination

    Use the Access Key and Access Secret created in step 5 to configure your AWS SQS Event Destination.