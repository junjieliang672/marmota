from abc import ABC, abstractmethod
import core.keys as keys

class BaseMarmotaManager(ABC):

    def __init__(self, marmota_path : str):
        self.marmota_path = marmota_path
        # _ denotes the internal variables, anyone should not directly read/modify
        self._is_marmota_running : bool = False

    @abstractmethod
    def start_marmota(self, start_dir : str = None, args : list[str] = None) -> None:
        pass 
    
    @abstractmethod
    def send_text_input(self, text : str, all_at_once : bool = False) -> None:
        pass 

    @abstractmethod
    def send_special_input(self, key : keys.Keys) -> None:
        pass 

    @abstractmethod
    def get_rendered_output(self) -> str:
        pass
    
    
    @abstractmethod
    def is_marmota_running(self) -> bool:
        """
        We allow using _is_marmota_running variable for efficiency
        But this method should give the true state, although this might have some calculations
        """
        return self._is_marmota_running
    
    @abstractmethod
    def close_marmota(self) -> None:
        """
        Close marmota if its running and cleanup any other resources
        """
    
    def runtime_info(self) -> str:
        return "[No runtime info]"

