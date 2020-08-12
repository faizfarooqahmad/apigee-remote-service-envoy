# Copyright 2020 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

import os
import requests
import subprocess
import time

class LegacyTestClient():
  def __init__(self, apigee_client):
    self.apigee_client = apigee_client
    self.key, self.secret = apigee_client.fetch_credentials()
    self.url = "http://localhost:8080/httpbin/headers"

  def test_apikey(self, logger, expect=200):
    apikey_header = {"x-api-key": self.key}
    response = requests.get(url=self.url, headers=apikey_header)
    status = response.status_code
    if expect == None:
      logger.debug(f"call using API key got response code {status}")
      return
    if status != expect:
      logger.error(f"failed to test target service using API key, expected {expect} got {status}")
    else:
      logger.debug(f"call using API key got response code {status} as expected")

  def test_jwt(self, cli_dir, logger, expect=200):
    org = os.getenv("ORG")
    env = os.getenv("ENV")
    logger.debug(f"fetching JWT from organization {org} and environment {env}")
    cmd = [f"{cli_dir}/apigee-remote-service-cli", "token", "create",
        "--legacy", "-o", org, "-e", env,
        "-i", self.key, "-s", self.secret]
    process = subprocess.run(cmd, capture_output=True)
    if process.stderr != b'':
      raise Exception("failed in fetching JWT" + process.stderr.decode())
    token = process.stdout[:-1].decode() # remove the line breaking
    auth_header = {"Authorization": f"Bearer {token}"}
    response = requests.get(url=self.url, headers=auth_header)
    status = response.status_code
    if expect == None:
      logger.debug(f"call using JWT got response code {status}")
      return
    if status != expect:
      logger.eror(f"failed to test target service using JWT, expected {expect} got {status}")
    else:
      logger.debug(f"call using JWT got response code {status} as expected")

  def test_quota(self, quota, logger):
    for _ in range(quota):
      self.test_apikey(logger, None)

    time.sleep(1)

    logger.debug("expecting this call to fail for quota depletion...")
    self.test_apikey(logger, 403)

    logger.debug("waiting for quota to be restored. this takes about a minute...")
    time.sleep(62)

    logger.debug("expecting this call to succeed with restored quota...")
    self.test_apikey(logger, 200)

  def test_local_quota(self, quota, logger):
    # turn the remote-service proxies offline
    logger.debug("turning the remote-service proxies offline...")
    try:
      response = self.apigee_client.undeploy_proxy()
      if response.ok == False:
        logger.error("turning the remote-service proxies offline")
        logger.error(response.content.decode())
    except Exception as e:
      logger.error(e)

    time.sleep(5)

    logger.debug("performing local quota test...")
    self.test_quota(quota, logger)

    # turn the remote-service proxies back on
    logger.debug("turning the remote-service proxies back on...")
    response = self.apigee_client.deploy_proxy()
    if response.ok == False:
      logger.error("turning the remote-service proxies back on")
      logger.error(response.content.decode())
