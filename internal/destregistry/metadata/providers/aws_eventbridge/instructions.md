# AWS EventBridge Configuration Instructions

[Amazon EventBridge](https://aws.amazon.com/eventbridge/) is a serverless event bus that lets you route events between your own applications, AWS services, and third-party SaaS applications using rules that you configure.

## How to configure AWS EventBridge as an event destination using the AWS CLI

To follow these steps you will need an AWS account, and the [AWS CLI](https://aws.amazon.com/cli/) installed and authenticated.

1. Create an event bus (optional)

    ```sh
    aws events create-event-bus --name BUSNAME --region REGION
    ```

    Skip this step to use your account's default event bus, and use `default` as BUSNAME in the next step.

2. Create a policy with the permissions needed to publish events

    ```sh
    aws iam create-policy --policy-name POLICYNAME --policy-document '{
      "Version": "2012-10-17",
      "Statement": [
        {
          "Effect": "Allow",
          "Action": [
            "events:PutEvents"
          ],
          "Resource": "arn:aws:events:REGION:ACCOUNTID:event-bus/BUSNAME"
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

6. Configure your AWS EventBridge Event Destination

    Use the Access Key ID and Secret Access Key created in step 5, along with your region and event bus name, to configure your AWS EventBridge Event Destination. Leave the event bus name empty to use the default event bus.
