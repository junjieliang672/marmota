from core.marmota_manager import BaseMarmotaManager
from core.fs_manager import TestFSManager

class Environment:
    """Manage test environment
    Manage cleanup of environment and other stuff at a single place
    """    
    def __init__(self, marmota_manager : BaseMarmotaManager, fs_manager : TestFSManager ):
        self.marmota_mgr = marmota_manager
        self.fs_mgr = fs_manager

    def cleanup(self) -> None:
        self.marmota_mgr.close_marmota()
        self.fs_mgr.cleanup()