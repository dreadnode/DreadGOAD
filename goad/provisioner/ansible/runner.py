import os
import time

import ansible_runner
from goad.utils import *
from goad.log import Log
from goad.provisioner.ansible.ansible import Ansible


class LocalAnsibleProvisionerEmbed(Ansible):
    provisioner_name = PROVISIONING_RUNNER

    def run_playbook(self, playbook, inventories, tries=3, timeout=30, playbook_path=None):
        if playbook_path is None:
            playbook_path = self.path + 'playbooks' + os.path.sep
        Log.info(f'Run playbook : {playbook} with inventory file(s) : {", ".join(inventories)}')
        Log.cmd(f'ansible-playbook -i {" -i ".join(inventories)} {playbook}')

        run_complete = False
        runner_result = None
        nb_try = 0
        # Exponential backoff: 10s, 30s, 60s
        wait_times = [10, 30, 60]

        while not run_complete:
            nb_try += 1
            runner_result = ansible_runner.run(private_data_dir=self.path + 'private_data_dir',
                                               playbook=playbook_path + playbook,
                                               inventory=inventories)
            if len(runner_result.stats['ok'].keys()) >= 1:
                run_complete = True
            if len(runner_result.stats['dark'].keys()) >= 1:
                wait_time = wait_times[min(nb_try - 1, len(wait_times) - 1)]
                Log.error(f'Unreachable vm, waiting {wait_time}s before retry (attempt {nb_try}/{tries})')
                time.sleep(wait_time)
                run_complete = False
            if len(runner_result.stats['failures'].keys()) >= 1:
                # Add exponential backoff for failures too
                if nb_try < tries:
                    wait_time = wait_times[min(nb_try - 1, len(wait_times) - 1)]
                    Log.error(f'Error during playbook iteration {str(nb_try)}, waiting {wait_time}s before retry')
                    time.sleep(wait_time)
                else:
                    Log.error(f'Error during playbook iteration {str(nb_try)}')
                run_complete = False
            if nb_try >= tries:
                Log.error(f'{tries} attempts failed, aborting.')
                break
        # print(runner_result.stats)
        return run_complete
