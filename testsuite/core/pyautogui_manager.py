import time 
import subprocess
import pyautogui
import core.keys as keys
from core.marmota_manager import BaseMarmotaManager

class PyAutoGuiMarmotaManager(BaseMarmotaManager):
    """Manage Marmota via subprocesses and pyautogui
    Cross platform, but it globally takes over the input, so you need the terminal 
    constantly on focus during test run
    """
    MARMOTA_START_DELAY : float = 0.5
    def __init__(self, marmota_path : str):
        super().__init__(marmota_path)
        self.marmota_process = None


    def start_marmota(self, start_dir : str = None, args : list[str] = None) -> None:
        marmota_args = [self.marmota_path]
        if args :
            marmota_args += args
        marmota_args.append(start_dir)

        self.marmota_process = subprocess.Popen(marmota_args,
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        time.sleep(PyAutoGuiMarmotaManager.MARMOTA_START_DELAY)

        # Need to send a sample keypress otherwise it ignores first keypress
        self.send_text_input('x')
        
    
    def send_text_input(self, text : str, all_at_once : bool = False) -> None:
        if all_at_once :
            pyautogui.write(text)
        else:
            for c in text:
                pyautogui.write(c)

    def send_special_input(self, key : keys.Keys) -> None:
        if isinstance(key, keys.CtrlKeys):
            pyautogui.hotkey('ctrl', key.char)
        elif isinstance(key, keys.SpecialKeys):
            pyautogui.press(key.key_name.lower())
        else:
            raise Exception(f"Unknown key : {key}") 

    def get_rendered_output(self) -> str:
        return "[Not supported yet]" 
    
    
    def is_marmota_running(self) -> bool:
        self._is_marmota_running = (self.marmota_process is not None) and (self.marmota_process.poll() is None)
        return self._is_marmota_running
    
    def close_marmota(self) -> None:
        if self.marmota_process is not None:
            self.marmota_process.terminate()
    
    # Override
    def runtime_info(self) -> str:
        if self.marmota_process is None:
            return "[No process]"
        else:
            return f"[PID : {self.marmota_process.pid}, poll : {self.marmota_process.poll()}]"  



